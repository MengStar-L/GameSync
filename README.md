## GameSync

这是一个**从零重建**的 `Go + Wails` 桌面项目，用来替代旧版 `GalFileSync`。

这次重构的原则是：
- **前端页面思路保留**：继续使用桌面侧边栏 + 卡片面板的交互节奏
- **同步后端全部重写**：不再依赖 WebDAV，改为 **Cloudflare D1 + R2**
- **支持多个 Cloudflare 账户**：每个游戏可绑定不同账号，以扩展免费存储空间
- **同步逻辑模仿 Steam Cloud**：通过“本地快照 / 上次同步锚点 / 云端版本”三方比对判断上传、下载与冲突

### 当前项目结构

- `main.go`：Wails 应用入口
- `app.go`：Wails 绑定层，负责前端调用、状态保存与同步调度
- `internal/core/models.go`：领域模型
- `internal/core/store.go`：本地 JSON 状态仓库
- `internal/core/cloudflare.go`：Cloudflare D1 / R2 客户端
- `internal/core/sync.go`：同步引擎（三方比对）
- `frontend/dist/`：静态前端资源

### 同步设计

#### 1. 数据分层
- **R2**：保存实际存档文件对象，按内容哈希组织对象 Key
- **D1**：保存每个游戏的当前 manifest、版本号和 revision 历史

#### 2. 多账户扩容
- 每个 `Game` 都有一个 `StorageAccountID`
- 每个 Cloudflare 账户都同时包含：
  - `Account ID`
  - `API Token`
  - `D1 Database ID`
  - `R2 Bucket`
  - `R2 Access Key ID`
  - `R2 Secret Access Key`
- 这样可以按游戏维度把不同游戏分散到不同 CF 账号

#### 3. Steam 风格判定
同步时会比较三份状态：
- **base**：上次本机同步成功后保存的锚点 manifest
- **local**：本次扫描到的本地 manifest
- **remote**：D1 里保存的最新云端 manifest

判定规则：
- 只有本地改了：**上传**
- 只有云端改了：**下载**
- 双方都改了且结果一致：直接接受
- 双方都改了且结果不同：标记为 **conflict**，由用户选择本地或云端版本

### 本地数据

程序运行时会把状态保存到用户配置目录：
- Windows：`%AppData%/GameSync/state.json`

当前为了先把架构跑通，Cloudflare 凭证是直接保存在 `state.json` 中的。
后续如果要做正式版，建议把敏感凭证迁移到系统凭据管理器。

### 启动方式

#### 先决条件
你本机需要先安装：
- Go 1.22+

#### 安装依赖
```powershell
go mod tidy
```

#### 开发运行
Wails 项目不能直接 `go run .`，需要带上正确的 build tag：

```powershell
go run -tags dev .
```

#### 生成可执行文件
```powershell
go build -tags production -o GameSync.exe .
```

#### 可选：安装 Wails CLI
如果你后面想切回标准 Wails 工作流，再安装 CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### 已知说明

- 当前工作区起点是**空目录**，所以这次是完整新建项目，而不是增量改造
- 我在这个环境里发现 **Go / Wails CLI 当前并未安装到 PATH**，因此本次提交重点是把项目源码和结构先完整落下，**还没有在本机实际编译验证**
- 当前前端采用 `frontend/dist` 的静态资源方案，目的是降低前期依赖，先把 Wails + Go + 同步核心打通

### 你接下来最值得继续做的两件事

- **第一步**：把 Go 和 Wails 安装好，直接启动这套新骨架
- **第二步**：补上 D1 migration、账户可用性检测、R2 垃圾对象回收和更细粒度的同步日志
