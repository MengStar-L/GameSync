<p align="center">
  <img src="./resource/im1.png" width="108" alt="GameSync logo">
</p>

<h1 align="center">GameSync</h1>

<p align="center">
  <strong>把 PC 游戏存档、封面资料、备份与云端同步放进一个干净的桌面应用。</strong>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=for-the-badge&logo=go&logoColor=white">
  <img alt="Wails" src="https://img.shields.io/badge/Wails-Desktop-DF0000?style=for-the-badge">
  <img alt="Cloudflare" src="https://img.shields.io/badge/Cloudflare-D1%20%2B%20R2-F38020?style=for-the-badge&logo=cloudflare&logoColor=white">
  <img alt="Windows" src="https://img.shields.io/badge/Windows-AMD64-0078D4?style=for-the-badge&logo=windows&logoColor=white">
  <img alt="Release" src="https://img.shields.io/github/v/release/MengStar-L/GameSync?style=for-the-badge&logo=github&label=Release">
</p>

<p align="center">
  <a href="#快速开始">快速开始</a>
  ·
  <a href="#功能特性">功能特性</a>
  ·
  <a href="#同步架构">同步架构</a>
  ·
  <a href="#自动更新">自动更新</a>
  ·
  <a href="#开发构建">开发构建</a>
</p>

---

GameSync 是一个面向 Windows 桌面的游戏存档管理与云同步工具。它用 Cloudflare D1 保存游戏配置、同步索引和 manifest，用 Cloudflare R2 保存实际存档对象、封面缓存和备份文件，让不同设备之间的游戏存档可以像 Steam Cloud 一样自动判断上传、下载与冲突。

它适合想把第三方游戏、独立游戏、模拟器存档或非 Steam 存档统一管理的人。你可以为每个游戏绑定不同的 Cloudflare 账号，把免费 R2 空间拆成多个存档池；也可以在程序里一键检查 GitHub Release 更新，下载完成后自动替换并重启。

## 工作流

| 场景 | GameSync 做什么 | 结果 |
| --- | --- | --- |
| 添加游戏 | 选择启动文件、存档目录、封面与同步账号 | 游戏进入统一面板，可随时启动、同步和备份 |
| 启动前检查 | 对比本地 manifest、上次同步锚点和云端 manifest | 自动决定下载云端、保留本地或提示冲突 |
| 手动同步 | 扫描存档目录并生成内容哈希 | 只上传变化对象，减少重复存储 |
| 存档备份 | 创建手动或自动备份，并记录云端路由 | 可以回滚到指定备份版本 |
| 软件更新 | 读取 GitHub Release 的 `latest.json` | 校验 SHA256 后自动安装并重启 |

## 功能特性

### Steam Cloud 风格同步

- 基于 `base / local / remote` 三方 manifest 判断变化来源。
- 支持仅本地变化时上传、仅云端变化时下载、双方一致时自动接受。
- 双方同时修改且结果不一致时进入冲突处理，由用户选择保留本地或云端。
- 每个游戏有独立同步锁，避免重复点击或并发任务写坏同一份存档。

### Cloudflare D1 + R2

- D1 保存游戏目录、账号目录、当前 manifest、版本号和修订信息。
- R2 保存真实存档文件对象，按内容哈希组织，重复文件不会反复上传。
- 支持多个 Cloudflare 账号，每个游戏可以绑定不同 R2 Bucket。
- 删除游戏或更新 manifest 后，会清理不再被引用的陈旧 R2 对象。

### 桌面游戏库

- 以卡片视图管理游戏，支持常玩、标签、搜索和排序。
- 支持 RAWG 游戏资料搜索，自动补充简介、发行时间、评分和标签。
- 支持 SteamGridDB 封面搜索，快速补齐竖版封面。
- 支持启动游戏并记录游玩时长，结束后可自动创建最新备份。

### 备份与恢复

- 支持为单个游戏创建自定义名称的存档备份。
- 支持从备份恢复指定存档版本。
- 支持导出和导入软件配置，迁移游戏列表、账号和偏好。
- 主账号可保存加密凭据备份，用于新设备恢复配置。

### 安全更新

- GitHub tag 会触发云端构建，自动发布一个包含主程序和 updater 的 Windows ZIP。
- 程序内检查更新时只接受 HTTPS GitHub Release 下载地址。
- 下载包会校验文件大小和 SHA256。
- updater 解压时会阻止路径逃逸，并在替换失败时回滚。

## 快速开始

### 1. 下载程序

前往 [Releases](https://github.com/MengStar-L/GameSync/releases/latest)，下载最新的 Windows 包：

```text
GameSync-vX.Y.Z-windows-amd64.zip
```

解压后运行：

```text
GameSync.exe
```

### 2. 准备 Cloudflare

你需要准备至少一个 Cloudflare 账号，并创建：

| 项目 | 用途 |
| --- | --- |
| Account ID | 访问 D1 与 R2 的账号标识 |
| API Token | 主账号访问 D1、验证账号和同步目录 |
| D1 Database ID | 保存游戏配置、manifest 和同步索引 |
| R2 Bucket | 保存存档对象、封面缓存和备份文件 |
| R2 Access Key ID | 以 S3 兼容协议访问 R2 |
| R2 Secret Access Key | R2 写入与下载所需密钥 |

首次启动时可以按欢迎引导添加主账号；后续可在“账号”页面继续添加副账号。

### 3. 添加游戏

在主界面点击“添加游戏”，填写：

| 字段 | 说明 |
| --- | --- |
| 游戏名称 | 面板显示名称 |
| 启动文件 | 游戏 `.exe` 或其他启动入口 |
| 存档目录 | 需要同步的本地存档文件夹 |
| 同步账号 | 此游戏使用的 Cloudflare 存储账号 |
| 冲突策略 | 手动选择、本地优先或云端优先 |

保存后即可进行首次同步。

## 同步架构

GameSync 的同步核心是“本地扫描 + 云端索引 + 内容对象”的组合。

| 层级 | 保存内容 | 位置 |
| --- | --- | --- |
| 本地状态 | 游戏列表、账号、偏好、同步锚点、窗口状态 | `%AppData%/GameSync` |
| D1 索引 | catalog、manifest、版本号、revision 历史 | Cloudflare D1 |
| R2 对象 | 存档文件、封面、备份 ZIP 或目录对象 | Cloudflare R2 |

同步时会生成当前存档目录的 manifest，并与上次成功同步保存的 anchor、D1 中的 remote manifest 比较：

| 判断结果 | 行为 |
| --- | --- |
| 只有本地变化 | 上传新对象，提交新的 D1 manifest |
| 只有云端变化 | 下载缺失对象，替换本地存档 |
| 本地与云端相同 | 更新锚点，无需传输 |
| 双方都变化且不一致 | 标记冲突，等待用户选择 |

## 自动更新

GameSync 使用 GitHub Releases 作为稳定更新通道。

1. 推送形如 `v0.1.1` 的 tag。
2. GitHub Actions 构建 `GameSync.exe` 与 `gamesync-updater.exe`。
3. 发布 `GameSync-v0.1.1-windows-amd64.zip`、`latest.json` 和 `checksums.txt`。
4. 程序内点击“检查更新”，发现新版本后下载并校验。
5. 点击“更新并重启”，updater 会替换程序文件并重新启动 GameSync。

发布细节见 [`docs/release.md`](./docs/release.md)。

## 开发构建

### 环境要求

| 工具 | 版本 |
| --- | --- |
| Go | 1.22+ |
| Node.js | 20+ |
| Wails CLI | v2.12.0+ |

### 本地运行

```powershell
git clone https://github.com/MengStar-L/GameSync.git
cd GameSync

cd frontend
npm install
npm run build
cd ..

go test ./...
wails dev
```

### 生成可执行文件

```powershell
wails build -clean
```

构建产物会生成到：

```text
build/bin/GameSync.exe
```

### 本地打包 Release

```powershell
go build -trimpath -ldflags "-s -w" -o build/bin/gamesync-updater.exe ./cmd/gamesync-updater
.\scripts\package-release.ps1 -Version "0.1.0" -Platform "windows-amd64"
```

## 目录速览

```text
app.go                         # Wails 绑定层、调度、窗口、托盘与运行时事件
internal/core/sync.go          # 存档同步引擎
internal/core/cloudflare.go    # D1 / R2 客户端与云端 catalog
internal/core/updater.go       # 程序内更新检查、下载与启动 updater
cmd/gamesync-updater/          # 独立更新器进程
frontend/                      # Vite 前端界面
scripts/                       # Release 打包与 latest.json 生成脚本
.github/workflows/release.yml  # tag 触发的 GitHub Release 构建流程
```

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 桌面框架 | Wails v2 |
| 后端 | Go |
| 前端 | Vite, Vanilla JavaScript, CSS, Lucide Icons |
| 云端索引 | Cloudflare D1 |
| 对象存储 | Cloudflare R2, S3 compatible API |
| 更新分发 | GitHub Actions, GitHub Releases |

## 使用提示

- `state.json`、`.env`、本地构建产物和密钥文件不应提交到仓库。
- 第一次同步前建议先确认存档目录是否正确，避免把游戏安装目录误当作存档目录。
- 如果多台设备同时修改同一游戏存档，优先使用手动冲突处理，确认后再覆盖。
- 本地开发构建默认没有更新源；只有 GitHub Actions 注入版本与更新地址后，程序内更新才会启用。

---

<p align="center">
  <sub>Made for a calmer game library and fewer lost saves.</sub>
</p>
