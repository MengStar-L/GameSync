# 同步缺陷修复规范（对应 sync-audit-2026-07-26.md）

每项给出**精确修法**。实现者必须先读审计报告拿到 file:line 证据锚点，修完逐项自查。
通用纪律：改动最小侵入；用户可见文案中文；Go 改完 `go build ./... && go test ./internal/core/...` 必须过；JS 改完 `node --check`。

## 阶段 Go-A（引擎与备份安全）：process.go / backup.go / sync.go / app.go（启动·备份·删除区域）

**A1（C1）进程监控超时不得伪造会话结束**
process.go：区分三种退出——真实结束（曾 onStart）/ 从未识别到进程（超时）/ ctx 取消。
仅真实结束调 onEnd；从未识别改调新回调 onMiss（或 onEnd 带 started=false 参数）。
app.go handleGameSessionEnded：未识别场景不记时长、不做自动备份，emit 新事件
`game:monitor_timeout`（payload {gameId}），日志说明。ctx 取消（应用退出）同样不做备份。

**A2（C2）启动预恢复三道门 + mtime 还原**
app.go prepareLatestAutoBackupForLaunch：
1. 门1-本地干净才允许快进：当前本地 manifest hash == game.Anchor.LastManifest.Hash（本地自
   anchor 后无改动）才可自动恢复；否则跳过预恢复，交由后续 InspectLaunchSync 正常冲突流程。
2. 门2-候选必须更新：候选备份的 SourceManifestGeneratedAt 晚于 anchor.LastManifest.GeneratedAt
  （用清单世代而非墙钟 CreatedAt 判新旧；缺失该字段的旧记录不自动恢复）。
3. 门3-恢复前安全备份：RestoreBackup 前对当前存档目录创建 safety 备份（type=manual、
   name="预恢复安全备份 <时间>"，不参与 auto 清理），失败则中止恢复。
backup.go extractBackupArchive：还原 zip 内文件 ModifiedAt（zip.FileHeader.Modified →
os.Chtimes），使哈希守卫跨设备生效，消除重复恢复。
latestReadyAutoBackupRecord 判"最新"改用 SourceManifestGeneratedAt 优先、CreatedAt 兜底。

**A3（M7）存档目录写入方纳入 per-game 锁**
lockGameSync 现仅 RunSync 使用。将以下三处包入同一把锁（注意不可跨 RunSync 持锁，避免自死锁：
各自独立短临界区）：prepareLatestAutoBackupForLaunch 的恢复段、handleGameSessionEnded 的
CreateBackup 段、RestoreGameBackup（app.go:3268 区域）。

**A4（M8）下载改为 CAS 成功后才提交**
sync.go：applyDownloads 的 rollback 不在函数内 commit；把 rollback 句柄上提到 runSync 主流程，
SaveRemoteManifestIfVersion 成功后才 commit；CAS 失败（ErrRemoteManifestChanged）时执行
rollback.restore() 把本地存档目录还原到同步前状态再返回错误——重试时不再产生虚假冲突。

**A5（M9）备份注册表与队列**
1. store.go mergeGameFields：BackupRegistry 不再整块按 StorageUpdatedAt 替换，改按 filename
   为键做并集合并（同名取状态更"进"的一方：ready > pending_upload；带 DeletedAt/PendingDelete
   的删除意图保留）。
2. app.go：自动备份本地旧 zip 清理从 CreateBackup 时移到"上传成功后"（cleanup 在 worker 成功
   回调里做）；上传失败保留旧 zip 与旧 ready 记录（persistBackupRecord 不再无条件清除全部 auto
   记录，仅在新记录 ready 后清除更旧的）。
3. 启动时重入：startup 里扫描全部游戏 BackupRegistry，把 pending_upload 重新入上传队列、
   pending_delete/DeleteRetryAt 到期的重新入删除队列。

**A6（minor-4/5）**
1. cleanupRemoteObjects：被替换对象不立即删，登记到延迟清理（记录 {sha, replacedAt} 于本地
   store 每游戏一个小列表，下次同步时删除 replacedAt > 10 分钟的）——给他机下载留宽限期。
2. SaveRemoteManifestIfVersion：读取 D1 meta.changes（Query 返回结构里有 changes/rows_written
   之类字段；若客户端未暴露则给 Query 结果补充解析），changes==1 即视为成功，不再依赖写后读
   比较；读后校验仅作兜底日志。

## 阶段 Go-B（目录与 LWW）：app.go（目录区域）/ store.go / cloudflare.go

**B1（放大器）后台拉取合并后必须通知前端**
runRemoteCatalogSync 与 syncLatestRemoteCatalog 的 pull 合并成功且状态有实际变化时
emitStateUpdated；processQueuedGameDelete 本地删除成功后、delete_failed 恢复后同样 emit。

**B2（M2）verifyAccounts 只回写验证结果字段**
不再整体 UpsertAccount：新增 store.UpdateAccountVerification(id, verificationState,
lastVerifiedAt, lastError, usageWarning, usedBytes, tokenExpiresAt, credentialsBackedUp)，
只改这些字段且**不触碰 CatalogUpdatedAt**。accountCatalogChanged 豁免清单补上
UsedBytes、TokenExpiresAt。

**B3（M4）RuntimeUpdatedAt 不清零**
cloudflare.go remoteCatalogGame：保留 RuntimeUpdatedAt（仅继续清 Anchor/LastSync）。
store.go normalizeGameCatalogTimestamps 的零值回填仅用于本地补齐历史数据，拉取合并路径
（mergeGameFields 前）不得把远端零值回填成 CatalogUpdatedAt——远端零值时 runtime 组永不获胜。

**B4（M5）push 丢失检测：pull-merge-repush 收敛环**
syncRemoteCatalog：push 完成后再 pull 一次并 MergeRemoteCatalog；若合并发现本地仍有比远端新的
字段（即 store 变 dirty 或合并产生本地胜出的差异），重新 push；最多循环 3 次，仍不稳定则保持
dirty 并 scheduleCatalogRetry。保证"UPDATE 被行级守卫静默 no-op"的一方最终把字段补回云端。

**B5（M11）revision 水位不吞并发**
push 后：读回 revision，若 == pull 时基线+1 → MarkCatalogSynced(读回值)；否则说明窗口内有他机
自增 → MarkCatalogSynced(pull 基线)（保守，下次必再 pull 合并）。

**B6（M6）删除链路**
1. B1 的 emit 已覆盖前端幽灵。
2. tombstone 判定改用"编辑时间"：新增 gameEditTimestamp(g) = max(Metadata/Tags/SyncConfig/
   Storage UpdatedAt)（不含 Runtime）。activeTombstoneMap 的跳过判定、MergeRemoteCatalog 的
   墓碑放行判定（store.go:390 区域）、push 侧 DELETE catalog_tombstones 的比较值全部改用它——
   纯 playtime/会话写入不再复活已删游戏。
3. SaveGame/UpsertGame：若该 id 在本地 tombstones 中且当前 games 列表无此游戏 → 返回错误
   "该游戏已在其他设备被删除"（阻止编辑页保存复活幽灵）。

**B7（M1 后端半区）偏好字段级基线 CAS**
store.go SavePreferences：对 FavoriteGames/PinnedTags/TagOrder/SidebarNavOrder 四个列表字段，
incoming 值与当前不同释义分两种：incoming 的对应 *UpdatedAt（客户端快照基线）>= 当前 *UpdatedAt
→ 真实新改动，接受并盖 now；< 当前 → 客户端基线陈旧，**保留当前值不覆盖**（不盖时间戳）。
标量字段（模式/密钥等）维持现状。前端 spread 快照自带基线时间戳，无需改绑定签名。

## 阶段 JS（四个并行代理，文件互不重叠）

**J1：store.js + main.js + mock.js**
1. 偏好动作串行化：toggleFavorite/pinTag/reorderTags/savePreferences 经同一 promise 链依次执行
  （前一个 applySnapshot 后再读 prefs 组装下一个），消除同机连点互滚。
2. 状态词表：main.js 映射补 queued→"排队中"、succeeded→"已同步"；chrome.css 若无对应 dot 样式
   由 J1 一并补进（succeeded 绿、queued 灰蓝）——注意 chrome.css 归 J1 专属修改。
3. 冲突对话框带游戏名：调 ui.conflictDialog(message, { gameName })（契约见 J3），syncGame 与
   launchGame 两处传入 game.name。
4. 清理路径：conflictDialog 取消分支补 setRuntimeStatus(null)+setNet("online","")；launchGame
   "跳过预同步"成功分支同样清 netStatus；新增 game:monitor_timeout 事件处理（清 runtimeStatus、
   toast "未能识别游戏进程，本次未记录时长" info）。
5. syncAll：循环内实时用 select.games() 校验游戏仍存在；计数改为收集本轮每个 syncGame 的实际
   结果（让 syncGame 返回 'success'|'conflict'|'failed'|'skipped'），不再读旧快照猜。
6. mock.js 对齐：catalog:sync_state 事件词改为 queued/syncing/succeeded 序列模拟真实后端；
   新增 game:monitor_timeout 可测钩子（如 mock 启动"尚未配置的游戏"时触发）。

**J2：views/game.js**
1. 脏字段提交：form 增加 dirty 集合，输入/开关/标签/封面等每次用户操作记录字段名；saveForm 的
   payload = 保存时最新 base 展开 + **仅 dirty 字段**覆盖；RAWG/SGDB 应用资料时把写入的字段全部
   记 dirty。零 dirty 时保存直接提示"没有修改"并返回，不发请求。
2. state:updated 期间表单未 dirty 的字段跟随最新值刷新显示（可选实现：仅在保存时取最新 base
   已满足正确性——至少保证正确性，显示刷新尽力而为）。
3. notFound 恢复：远端删除清空页面前若表单有 dirty，先 ui.confirm 提示"游戏已在其他设备被删除，
   放弃未保存的修改？"（仅提示，无法阻止删除）；notFound 后订阅不再永久短路——游戏重新出现时
   自动重挂载（对齐 mount 早期路径行为）。

**J3：views/accounts.js + ui.js**
1. accounts.js 脏字段提交：抽屉输入记录 dirty；保存 payload = store.select 最新账号展开 + 仅
   dirty 字段；enabled 开关同理。保存时若账号已被远端删除 → toast 错误并关抽屉。
2. ui.js conflictDialog(message, opts={gameName})：有 gameName 时标题显示
   `「gameName」存在同步冲突`；正文 message 不变。保持旧调用（单参）兼容。
3. ui.js Escape 只关顶层：维护 overlay 打开栈，keydown 处理时仅栈顶 overlay 响应并
   stopImmediatePropagation；其余不动。

**J4：views/library.js**
拖拽结束的 finishDrag：不再要求长度相等——以 DOM 顺序为主序，store 中 DOM 缺失的 id（远端新增）
按其在 store 的相对位置插入结果数组（简化：追加到原相邻元素之后或队尾），DOM 中多出的已删 id 过滤
掉；顺序与 store 相同才跳过提交。拖拽被远端变更打断导致回弹时 toast 提示"列表已被其他设备更新"。

## 核验清单（最终核验代理逐项打钩）
C1 C2 C3 / M1-M11 / minor1-8 每项：改动落点、机制说明、构建与测试通过、（前端）语法通过。
