# 同步逻辑审计报告（2026-07-26）

范围：同步状态顺序正确性 + 多设备一致性。方法：5 维度并行审查（Go 同步引擎 / 目录多设备 / 备份管线 / 前端 store / 前端视图），每条发现经独立对抗验证员逐行证伪核实。以下均为**已确认**的缺陷（标注 file:line 为证据锚点）。

## 总体结论

- **单设备串行使用**：基本可正常同步。引擎骨架是对的——anchor 只在成功路径前移、下载有回滚保护、D1 清单更新带 CAS 乐观锁、目录 dirty 标记与认证失败重试链路健全。
- **多设备**：存在系统性缺陷，三条根因贯穿全部高危问题：
  1. **启动预恢复/自动备份旁路完全绕过冲突判定**——真实丢档链路（2 个 critical）；
  2. **"整包读-改-写 + 按差异盖新时间戳"的伪 LWW**——旧数据被盖上新时间戳后反向覆盖新数据并传播到所有设备（1 critical + 4 major）；
  3. **云端 D1 是行级整 blob LWW，而客户端是字段级 LWW**，粒度不匹配 + revision 水位记账缺陷 → 并发修改静默丢字段、目录分叉。

---

## Critical（丢档风险，建议最优先修复）

### C1. 进程监控 60 秒超时即触发"会话结束备份"，游戏还在运行就打包旧存档
`internal/core/process.go:63` + `app.go:2794`
监控 goroutine 的 defer 对所有 return 路径无条件调用 onEnd，包括 60 秒扫描超时与 ctx 取消。当 InstallPath 指向启动器桩/快捷方式/steam:// 类目标时，扫描永远匹配不到真实游戏进程 → 游戏刚开始玩，后台就执行了"会话结束自动备份"（内容是开局前的旧档）。叠加 C2 的预恢复，下次启动会用这个假"最新"备份覆盖真实进度。

### C2. 启动预恢复无新旧比较、无冲突门控、无恢复前安全备份
`app.go:1656`（prepareLatestAutoBackupForLaunch）→ `backup.go:546-556`
恢复决策只看 registry 里 CreatedAt 最大的 ready 自动备份 + 一个哈希相等守卫，**不与本地存档新旧比较、不走 anchor/冲突判定**。恢复是整目录原子替换，rollback 副本随手删除；下一次 CreateBackup 还会删掉其它全部 `backup_auto_*.zip`（`backup.go:119-126`）——保存最新进度的唯一 zip 被清除，**最新进度全局永久丢失**。加重因素：`extractBackupArchive` 不还原文件 mtime（`backup.go:560-600`），而守卫哈希包含 ModifiedAt，跨设备场景守卫恒为 false，每次启动都会重复执行整目录恢复。另：备选判定用跨设备墙钟 CreatedAt（`app.go:1636`），设备时钟偏差会选中内容更旧的备份（已确认，minor 加重项）。

### C3. 游戏详情页表单整包保存，零修改点保存即可回滚另一设备的全部编辑并全网传播
`frontend/src/views/game.js:463-496` + `app.go:460-494` + `internal/core/store.go:1163-1179`
表单只在挂载时从 store 拷贝一次，此后不随远端更新回写；保存时用挂载旧值覆盖 base 新值整包提交。后端把 incoming 与当前值比对，有差异就把 Metadata/Tags/SyncConfig/Storage 组级时间戳全部提为 now → 陈旧表单值获得最新时间戳，经 LWW 在**所有设备**上无声回滚对端编辑。组级时间戳还放大破坏面：只合法改了名称，也会把对端改的描述/封面/评分整组回滚。

---

## Major（状态错乱 / 数据回滚 / 同步失败）

### M1. 偏好整包读-改-写：跨设备覆盖收藏/固定标签/排序
`frontend/src/store.js:282,329,371` + `store.go:651-680`。前端用可能过期的快照 `{...prefs(), 字段}` 整包提交；后端只与"后端当前值"比差异就盖 time.Now()，无法区分用户新改动与陈旧回传。放大器：后台目录 pull 合并后**从不发 state:updated**（`app.go:2567-2616` 全路径只发 catalog:sync_state），前端快照的陈旧窗口一直持续到下一次返回快照的 RPC。同机快速连点两次收藏也会互滚（两次调用都基于同一份旧 prefs）。

### M2. verifyAccounts 用启动快照整体回写账号 + 盖新时间戳，回滚他机账号配置并全网传播
`app.go:177 / 242 / 2733-2745` + `store.go:147-151`。启动时 verifyAccounts 与 bootstrap pull 并发；验证耗时数秒，期间 pull 合并的他机新配置（如 R2Bucket）被验证结束后的整体写回冲掉，且 accountCatalogChanged 未豁免 UsedBytes/TokenExpiresAt（每次验证必变）→ 常态化给未变配置盖新时间戳。后果：备份可能被路由到已弃用的旧 bucket。

### M3. 账号编辑抽屉整包保存：复活/回滚他机的 enabled、R2Bucket 变更
`frontend/src/views/accounts.js:231-243`。同 C3 机制，抽屉长开期间他机改动被打开时刻的快照+输入初值覆盖。

### M4. RuntimeUpdatedAt 推送清零、拉取回填：他机改个名就回滚你的游玩时长
`cloudflare.go:370-372` + `store.go:1143-1145,1216-1220`。推送时 RuntimeUpdatedAt 清零但 PlayTime/LastPlayed 保留；拉取侧把零值回填为整条 CatalogUpdatedAt → 陈旧 PlayTime 伪装成新鲜数据，覆盖本地更新的游玩记录且推回云端，不可恢复。

### M5. D1 行级整 blob LWW 与客户端字段级 LWW 粒度不匹配：并发改不同字段，一方静默丢失
`cloudflare.go:254-262（games_catalog）/ 329-334（app_preferences）`。行内细粒度 *UpdatedAt 在 D1 层不参与判定；UPDATE 被 WHERE 守卫静默 no-op 后照常 MarkCatalogSynced 清 dirty——落败方自认为同步成功，其修改**无限期**对全网不可见（只有它自己再改任意字段才顺带补上）。

### M6. 删除游戏链路：幽灵条目 + 跨设备复活
- 前端：删除成功事件只清 pendingDeletes，快照 games 从未刷新（删除路径无 emitStateUpdated，`app.go:774-811`）→ 已删游戏在下一次重渲染时复现，可被点同步（报错）甚至被详情页保存**重建**（UpsertGame 新增分支先删 tombstone，`store.go:524`）→ 全网复活。
- 后端：A 删除时 B 恰在游玩，B 会话结束的 playtime 自动写入把 CatalogUpdatedAt 抬过 tombstone 的 deletedAt → 穿透墓碑判定（`store.go:390` / `app.go:2531-2540` / `cloudflare.go:265`），游戏以空路径幽灵状态在所有设备复活。

### M7. 存档目录写入方不持锁：与同步交错 → R2 对象内容与清单 SHA 不符
`app.go:1241` 是唯一取锁点（RunSync）；启动预恢复、会话结束自动备份、手动恢复备份均无锁。与进行中同步交错时，上传以清单里的旧 SHA 命名对象但读的是新内容（`sync.go:343`）→ 他机下载哈希校验失败（`sync.go:568`），**持续同步失败**直到该文件再次变更。

### M8. 下载先于 CAS 提交且不可回滚：版本竞争产生虚假冲突，诱导用户覆盖他机新数据
`sync.go:222-250`。CAS 失败时 anchor 不前移但下载已落盘 → 重试时对用户从未碰过的文件弹冲突框；按直觉选"保留本地"会把过期内容作为新版本覆盖他机刚推的数据。配合 M10（冲突框不写游戏名）风险放大。

### M9. BackupRegistry 跨设备整块 LWW 互抹 + 上传失败先删旧后作废 + 队列纯内存
- registry 按 StorageUpdatedAt 整块替换而非并集（`store.go:1207-1215`）：并发备份记录互相抹除；CleanupCloudAutoBackups 可级联删除他机较新的云端自动备份。
- 上传失败路径先删了本地旧 auto zip、又无条件清掉上一条 ready 记录（`app.go:3104-3140`）。
- 上传/删除队列纯内存、重启不重入：pending_upload/pending_delete 永久卡死，DeleteRetryAt 无消费者。

### M10. 冲突对话框不标识游戏：syncAll 多冲突连续弹出雷同对话框，用户可能对错误的游戏选"保留本地"
`ui.js:273` 标题硬编码 + `sync.go:200` message 无游戏名 + `store.js:218-224` 串行循环。选错的直接后果是单侧覆盖该游戏云端较新存档。且 overlay 可叠开、每层各挂 document 级 Escape 监听，一次 Esc 关闭全部叠层并静默取消底层冲突。

### M11. MarkCatalogSynced 把推送后的 revision 当作已拉取水位
`app.go:2513,2699` + `cloudflare.go:432-442`。increment 与读回是两次独立 HTTP，读回值可含他机自增 → 并发窗口内他机写入被"视为已同步"，pull 被 revision 相等短路。会因本机任何后续变更自愈，但跨重启前设备可停留在旧目录视图。

---

## Minor（体验 / 显示）

1. **catalog:sync_state 词表脱节**：后端发 queued/succeeded，前端映射表没有 → 每次操作后顶栏常驻"状态未知"（真实后端 100% 复现；mock 用的是另一套词表所以开发时看不到）。`store.js:426` / `main.js:108-117` / `app.go:2561,2612`
2. 冲突对话框"取消" / 启动"跳过预同步"成功路径不清理状态：卡片永久挂"存在冲突"、顶栏错误显示"离线"或卡"同步中"。`store.js:188,262-264`
3. syncAll 成功计数读旧快照：RunSync reject 时上一轮的 success 被计为本轮成功，可能显示"全部成功"实际 0 成功；数组一次性捕获不随中途变更刷新。`store.js:210-224`
4. CAS 成功后立即删除被替换的 R2 对象，无宽限期：打断他机进行中的下载（有回滚不丢数据，但确定性失败）。`sync.go:250,398-407`
5. SaveRemoteManifestIfVersion 写后读校验：自己已生效的提交可能被误判为冲突失败（虚假失败 + 孤儿对象）。`cloudflare.go:683-705`
6. 拖拽排序期间远端目录变化：整次拖拽被静默丢弃，或提交过期 ids 把他机新加的游戏挤到队尾。`library.js:350-366`
7. 远端 tombstone 到达即清空详情页（未保存输入无确认丢失），notFound 后永久短路不可恢复。`game.js:1024-1030`
8. 详情页编辑期间 select.game 不过滤 pendingDeletes 等次要口径问题。

## 验证中被证伪/修正的项（说明审计可信度）

- "自动备份 pending_upload 分支导致预恢复选中旧备份"的子链被证伪（persistBackupRecord 会先清 auto 记录），但 C2 主链独立成立。
- "RuntimeUpdatedAt omitempty 序列化即消失"表述不准（Go 对 time.Time 结构体不生效），但 IsZero 回填逻辑照样触发，结论不变。
- revision 短路"永久分叉"被降级：本机任何变更即自愈（M11）。
- R2 即时删除从 major 降为 minor：有回滚保护，不丢数据。

## 建议修复顺序

**P0（丢档链路，先堵）**
1. C1：进程监控超时/取消不得触发"会话结束备份"（超时应放弃本次监控，不产生备份与时长）。
2. C2：预恢复加三道门：恢复前先做本地安全备份（且不参与 auto 清理）；备份必须证明比本地新（与 anchor.LastManifest 比较，而非墙钟/哈希不等）；否则走正常冲突流程让用户选择。解压还原 mtime。
3. C3/M1/M3：前端改为**脏字段提交**（只提交用户实际改过的字段，保存时与最新 base 合并）；后端提供字段级 patch 语义或按 *UpdatedAt 基线校验拒绝陈旧整包。

**P1（多设备一致性）**
4. M2：verifyAccounts 只回写验证结果字段（verificationState/lastVerifiedAt/usedBytes 等），不整体 Upsert；豁免字段修正。
5. M4：推送不清零 RuntimeUpdatedAt（或拉取侧不回填）。
6. M5/M11：D1 层改为字段组级列存或 push 前强制 pull-merge + revision CAS 重试；push 失败（no-op）要检测并保持 dirty。
7. M6：删除路径补发 state:updated；tombstone 判定豁免纯 runtime 写入；UpsertGame 区分"编辑已有"与"复活已删"。
8. M7/M9：备份/恢复/预恢复全部纳入 lockGameSync；registry 按 (deviceId, filename) 并集合并；队列持久化重入。

**P2（体验）**
9. 状态词表补全 + 冲突对话框标题带游戏名与双侧时间 + Esc 只关顶层 + 取消路径清理状态 + syncAll 口径修正。
