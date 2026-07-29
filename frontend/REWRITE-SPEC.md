# GameSync 前端全量重写规范（v3 · Paper Atelier）

旧前端（单文件 app.js + 侧栏布局 + 玻璃拟态）已整体删除。本次为白纸重写：
新信息架构、新视觉语言、新代码架构。唯一继承物是 Go 后端契约（wailsjs 生成层）。

## 1. 视觉语言 Paper Atelier（暖纸画廊）

关键词：**暖纸、墨色、朱砂、衬线、印刷感**。像一本精装游戏杂志，不像 Dashboard。

- 画布：暖纸色 `#f7f4ee`，上面铺极淡的纸纹噪点（tokens.css 提供 `--paper-grain` SVG data-URI）
  与两三块低饱和色域晕染（base.css 实现，几乎不可察觉）。
- 卡片 = 纸片：白底 `#fffdf9`、1px 墨色细边 `--line`、**硬偏移阴影**（`--shadow-press`：
  4px 4px 0 墨色 5% 透明，无模糊）。悬停：卡片上移 2px、阴影拉长到 6px 6px 0（`--shadow-press-hover`）。
  这是本设计的签名效果，禁止改成模糊柔影。
- 主色：朱砂橙 `--vermilion: #d9481c`（按钮/高亮/选中态/下划线）。辅助色域：
  鼠尾草绿 `--sage`（成功）、赭金 `--ochre`（警告）、绯红 `--crimson`（错误）、青蓝 `--indigo-ink`（信息/同步）。
- 文字：墨色 `--ink: #211d16`；次级 `--ink-2: #6b6355`；弱 `--ink-3: #a39a89`。
- 字体：Latin 展示字体 **Fraunces**（衬线，变量字重，`--font-display`），用于大标题、数字、
  英文品牌词；中文标题回落雅黑加粗。正文 `--font-body`（系统栈）。数字一律
  `font-variant-numeric: tabular-nums`。
- 圆角克制：`--radius: 10px`（卡片）、`--radius-lg: 14px`（大面板）、按钮 8px、chip 全圆。
- 动效：`--ease: cubic-bezier(0.22,1,0.36,1)`、`--ease-spring: cubic-bezier(0.34,1.56,0.64,1)`；
  时长 `--t-fast:130ms --t-med:220ms --t-slow:380ms`。页面切换 = 淡入+上升 12px；
  网格子项交错升入（nth-child 1..12 每项 +30ms）；导航页签下划线滑动；
  按钮按下 scale(0.97)；hover 才浮起。`prefers-reduced-motion` 全局收敛（base.css 统一处理）。

## 2. 信息架构（与旧版彻底不同）

无侧栏。**单条顶部带 Chrome**（高 52px，兼作无边框窗口标题栏）：

```
[◆ GameSync 品牌]  [游戏库] [标签] [云账号] [动态] [设置]   (拖拽空隙)   [搜索胶囊] [同步全部⟳] [状态点] [—][□][×]
```

页面（router key）：
- `library` 游戏库：精选主视觉（最近游玩的游戏大横幅：模糊封面底、衬线大标题、
  启动/详情/同步按钮、时长与备份数）＋ 筛选纸签行（全部 / ♥常玩 / 各固定标签 chip）＋
  封面墙（封面 3:4 纸片卡，**标题与元信息排在封面下方**，悬停浮起+封面内微缩放+出现快速启动按钮；
  支持长按拖拽排序、右键菜单）。搜索时网格实时过滤。
- `game` 游戏详情页（`{ id }` 或 `{ id: null }`=新建）：非弹窗，整页路由。
  顶部返回条 + 大封面 + Fraunces 标题 + 统计带（总时长/上次游玩/备份数/上次同步）；
  下方两个分区页签：**资料**（描述阅读/编辑、RAWG 拉取、SGDB 封面、平台开关、路径、
  账号、冲突策略、标签编辑）与**存档备份**（列表/新建/恢复/删除，进度与事件驱动刷新）。
  RAWG/SGDB 搜索用居中对话框。
- `tags` 标签：标签纸片墙（数量徽记、固定/取消固定 → 固定标签会出现在库页筛选行），右键菜单。
- `accounts` 云账号：账号"票券"卡（撕票齿孔分隔线视觉），验证/编辑/删除/启用；
  新建与编辑用右侧滑出抽屉表单（drawer）。
- `activity` 动态：顶部统计带（Fraunces 大数字：总游戏/成功率/待处理冲突/云端用量）＋
  垂直时间线（状态色点 + 纸片条目）。
- `settings` 设置：左锚点目录 + 右分区（本地路径/设备信息/软件更新/同步偏好/架构说明/备份与恢复）。
- 欢迎流程（首启）：全屏接管三选卡（从备份恢复 / 手动配置 / 跳过）。

全局层：toast（底部居中墨色胶囊，白字，左侧状态色点，入场弹起）、confirm 对话框、
冲突选择对话框（保留本地/保留云端/取消）、右键菜单（纸片+硬阴影）、右侧 drawer。

## 3. 代码架构

```
frontend/
  index.html            壳：chrome 带 + #view + 全局层根节点；只此一个 HTML
  css/
    tokens.css          全部设计令牌 + @font-face（唯一变量来源）
    base.css            reset/画布/排版/滚动条/reduced-motion/页面过渡
    chrome.css          顶部带（品牌/导航/搜索/窗控）
    components.css      按钮/输入/chip/badge/卡片基类/toast/dialog/drawer/context-menu/骨架屏
    views/{library,game,tags,accounts,activity,settings,welcome}.css
  src/
    main.js             boot：Bootstrap→store、事件绑定、路由挂载、welcome 判断
    api.js              后端桥。真实 Wails 绑定；window.go 缺失时自动换 mock.js（浏览器试驾）
    mock.js             全部绑定的假实现 + 样例数据 + 假事件流
    store.js            状态容器 + 订阅 + 业务动作（sync/launch/conflict/favorite/delete…）
    router.js           内部路由（无 hash）：navigate(page, params)、onChange、back()
    ui.js               DOM 工具 h()、icons、toast()、confirm()、dialog()、drawer()、
                        contextMenu()、格式化工具、封面 <img> 懒解析
    views/*.js          每页一个模块
```

### 3.1 视图模块接口（所有 views/*.js 必须遵守）

```js
// 每个视图导出唯一函数；root 为空容器，由 router 调用
// ctx = { store, api, ui, router }
// 返回 cleanup 函数（解除订阅/计时器）；router 切页时先调 cleanup 再清空 root
export function mount(root, ctx) {
  const off = ctx.store.subscribe(rerender)
  ...
  return () => off()
}
```

- 视图内部自行局部重渲染（对 store 变更做全量 innerHTML 重建即可，除非注明例外）。
- 视图不得 import 其他视图；跨页跳转一律 `router.navigate()`；共享逻辑放 store/ui。
- 每个视图的 CSS 类名前缀 = 视图名（`.lib-*` `.gd-*` `.tags-*` `.acc-*` `.act-*` `.set-*` `.wel-*`），
  杜绝跨文件选择器冲突。通用组件类（`.btn` `.chip` `.card` `.field` …）只在 components.css 定义。

### 3.2 store.js（已由核心实现，视图只消费）

```js
store.state        // { snapshot, runtimeStatus:{[gameId]:{text,tone}}, netStatus:{state,message},
                   //   search, libraryFilter:{kind:'all'|'fav'|'tag', tag}, booted }
store.subscribe(fn)            // 返回取消函数；状态变化后微任务合并触发
store.select.games()           // 排序后的游戏列表（快照顺序）
store.select.game(id)
store.select.filteredGames()   // 按 search + libraryFilter
store.select.favoriteIds()     // Set
store.select.tagSummaries()    // [{name,count,pinned}] 按 tagOrder
store.select.accounts() / activities() / preferences() / device() / dataDir()
store.select.heroGame()        // 精选：最近游玩，否则第一款
// 动作（全部 async、内部处理 toast/冲突对话框/快照应用；视图直接调用）：
store.actions.syncGame(id) / syncAll() / launchGame(id)
store.actions.toggleFavorite(id) / deleteGame(id)  // deleteGame 内部先 confirm
store.actions.saveGame(game) / reorderGames(ids)
store.actions.pinTag(name,on) / reorderTags(names)
store.actions.saveAccount(acc) / verifyAccount(id) / deleteAccount(id)
store.actions.savePreferences(patch)
store.actions.setSearch(q) / setLibraryFilter(f)
```

### 3.3 ui.js 提供（视图禁止自造同类轮子）

```js
h(tag, attrs, ...children)        // DOM 构建；attrs 支持 class/dataset/on 事件/html
icon(name, cls?)                  // lucide SVG 字符串
fmtTime(iso) fmtDuration(min) fmtBytes(n) esc(s)
toast(message, tone='ok'|'warn'|'err'|'info')
confirm({ message, confirmText, cancelText, tone }) -> Promise<boolean>
dialog({ title, width, render(body, close), onClose }) -> { close }   // 居中纸片对话框
drawer({ title, width=520, render(body, close) }) -> { close }        // 右侧滑出
contextMenu(event, items)         // items: [{label, icon, danger, onClick} | 'divider']
coverImg(refOrUrl, cls)           // 返回 <img>，内部经 api.resolveCover 缓存解析，失败显示纸纹占位
skeleton(cls)                     // 骨架屏块
```

### 3.4 api.js 表面（真实/mock 同构）

`api.<每个绑定名>(...)`（同 wailsjs App.d.ts 全集）＋ `api.onEvent(topic, fn)`（EventsOn 封装）
＋ `api.resolveCover(ref)`（带缓存的 ResolveCoverSource，http/data 直通）＋ `api.isMock`。

## 4. 后端契约速记（语义，必须精确遵守）

- 启动：`PrepareGameLaunch(id, conflictChoice) -> {snapshot?, status, message}`；
  `status==='needs_choice'` → 冲突对话框（选择后带 choice 重调）；`'failed'` → 抛错；
  出错时 confirm"跳过预同步继续启动？"；确认后 `LaunchAndMonitorGame(id)`。无 installPath 的游戏不能启动。
- 同步：`RunSync({gameId, conflictChoice}) -> snapshot`；同步后该游戏 `lastSync.status==='conflict'`
  且未带 choice → 冲突对话框，choice 为 `'local' | 'remote'`。
- 封面：`game.coverPath` 若以 http/data 开头直接用；否则以 `game.id` 调 `ResolveCoverSource` 换 src
  （必须缓存 + 并发去重）。RAWG/SGDB 返回的 coverPath/coverOptions 是直链。
- 删除游戏：用 `RequestDeleteGame(id)`（队列化）；随后事件 `game:delete_queued/succeeded/failed`
  （failed 且 stage==='remote_cleanup' 视为本地已删）。乐观隐藏卡片，failed 时恢复。
- 快照应用：所有返回 `DashboardSnapshot` 的动作，成功后 `store` 整体替换 snapshot 并通知。
- 事件（`api.onEvent`）：`game:started`(gameId) `game:ended` `game:backup_starting`
  `game:backup_success` `game:backup_error` `game:backup_upload_failed/succeeded`
  `game:backup_delete_failed/succeeded`（payload 可能是 string 或 {id|gameId,...}，用
  store 内 eventGameId 归一）；`sync:progress`({gameId?,message}) `cover:warning`({message})
  `catalog:sync_state`({status,message}) `catalog:sync_failed` `state:updated`(AppState 整包替换)。
- 运行状态胶囊（library 卡片与 hero 上）：由 store.runtimeStatus 驱动，tone:
  playing（青蓝）/syncing（青蓝呼吸）/success（鼠尾草，3s 自清）/warn。
- 账号：第一个为主账号（D1 索引中心），`SaveAccount` 后端自动命名；`VerifyAccount(id)`
  期间卡片进入验证中态；`verificationState==='valid'` 且无 lastError 才算有效。
- 偏好字段见 models.ts Preferences（favoriteGames/pinnedTags/tagOrder 都在偏好里，
  动作层负责读改写回 SavePreferences）。
- 更新：`CheckForUpdates` → status（`update_available` 等）→ `DownloadUpdate` → `ApplyUpdateAndRestart`。
- 首启：`IsFirstLaunch()` true → welcome 接管；恢复= `ImportAppBackup()`；手动配置 →
  跳转 accounts 页并自动开新建抽屉；跳过直接进库。
- 窗控：最小化 `WindowMinimise`、最大化切换 `WindowToggleMaximise`、关闭 `WindowHide`（隐藏到托盘）。

## 5. 铁律

1. 视觉必须遵守第 1 节令牌与签名效果（硬偏移阴影、纸感、朱砂主色、Fraunces 展示字体）。
2. 所有颜色/圆角/阴影/缓动引用 tokens.css 变量；禁止硬编码色值。
3. 视图之间零 import、CSS 前缀隔离；通用组件只出现在 components.css / ui.js。
4. 所有用户可见文案为简体中文；日期用 fmtTime、体积用 fmtBytes。
5. 空状态必须设计（无游戏/无标签/无账号/无动态/搜索无结果），用纸纹插画感 + 引导按钮。
6. 每个视图先渲染骨架屏再填充异步数据（如备份列表）。
7. 桌面窗口 ≥1200px，无需移动端；但 1200~2560px 网格需自适应。
8. mock 模式（浏览器）下所有页面与流程必须可完整走通 —— 这是验收方式。
