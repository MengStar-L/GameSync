<p align="center">
  <img src="./resource/im1.png" width="108" alt="GameSync logo">
</p>

<h1 align="center">GameSync</h1>

<p align="center">
  <strong>把 PC 游戏存档、封面、备份和多设备同步整理到一个干净的桌面应用里。</strong>
</p>

<p align="center">
  <img alt="Windows" src="https://img.shields.io/badge/Windows-AMD64-0078D4?style=for-the-badge&logo=windows&logoColor=white">
  <img alt="Cloudflare" src="https://img.shields.io/badge/Cloudflare-D1%20%2B%20R2-F38020?style=for-the-badge&logo=cloudflare&logoColor=white">
  <img alt="Auto Update" src="https://img.shields.io/badge/Auto%20Update-GitHub%20Release-181717?style=for-the-badge&logo=github">
  <img alt="Release" src="https://img.shields.io/github/v/release/MengStar-L/GameSync?style=for-the-badge&logo=github&label=Release">
</p>

<p align="center">
  <a href="#适合谁">适合谁</a>
  ·
  <a href="#主要功能">主要功能</a>
  ·
  <a href="#下载安装">下载安装</a>
  ·
  <a href="#首次配置">首次配置</a>
  ·
  <a href="#日常使用">日常使用</a>
  ·
  <a href="#数据安全">数据安全</a>
  ·
  <a href="#常见问题">常见问题</a>
</p>

---

GameSync 是一个 Windows 桌面工具，用来管理那些没有稳定云存档的 PC 游戏、独立游戏、第三方平台游戏、模拟器存档或手动安装的游戏。你可以把每个游戏的存档目录登记进 GameSync，然后让它帮你完成同步、备份、恢复和冲突处理。

它的目标很简单：少一点手动复制存档，少一点“我到底哪台电脑上的存档更新”的焦虑。

## 适合谁

- 你经常在多台 Windows 电脑之间玩同一个游戏。
- 你有不少非 Steam 游戏、独立游戏、Galgame、模拟器游戏或手动安装游戏。
- 你希望像 Steam Cloud 一样同步存档，但又想自己掌控云端存储。
- 你想给重要存档留手动备份和自动备份，必要时可以回滚。
- 你想给游戏库补封面、标签、简介，并在一个界面里启动和整理。

## 主要功能

### 存档同步

- 为每个游戏指定本地存档目录。
- 手动同步，或在启动游戏前自动检查云端是否有新存档。
- 只上传发生变化的文件，减少重复上传和存储占用。
- 本地和云端都改动时，会提示冲突，由你选择保留本地或云端。

### 备份与恢复

- 给单个游戏创建手动备份。
- 游戏结束后可生成自动备份。
- 从备份恢复到指定版本。
- 配置备份可用于迁移游戏列表、账号设置和偏好。

### 游戏库整理

- 以卡片形式管理游戏。
- 支持标签、常玩游戏、搜索和排序。
- 可记录游玩时长和最近游玩时间。
- 可使用 RAWG 补充游戏资料，使用 SteamGridDB 搜索竖版封面。

### 自动更新

- 在程序内点击“检查更新”即可查看新版本。
- 下载完成后点击“更新并重启”，程序会自动替换文件并重新打开。
- 更新包会校验文件大小和 SHA256，降低损坏包或错误包带来的风险。

## 下载安装

1. 打开 [最新版本下载页](https://github.com/MengStar-L/GameSync/releases/latest)。
2. 下载 Windows 压缩包：

```text
GameSync-vX.Y.Z-windows-amd64.zip
```

3. 解压到一个普通目录，例如：

```text
D:\Apps\GameSync
```

4. 双击运行：

```text
GameSync.exe
```

为了让程序内自动更新顺利工作，建议把整个解压目录保留在一起，不要单独移动或删除其中的更新器文件。也建议不要放在 `C:\Program Files` 这类需要管理员权限的目录里。

## 首次配置

GameSync 使用 Cloudflare D1 保存游戏目录和同步索引，使用 Cloudflare R2 保存真实存档文件、封面和备份。第一次使用前，你需要准备一个 Cloudflare 账号。

### 需要准备的内容

| 需要复制的字段 | 在 GameSync 中的用途 |
| --- | --- |
| Account ID | 标识你的 Cloudflare 账号 |
| API Token | 主账号访问 D1、验证账号、同步目录 |
| D1 Database ID | 保存游戏列表、同步版本和云端索引 |
| R2 Bucket | 保存存档对象、封面缓存和备份文件 |
| R2 Access Key ID | 上传和下载 R2 文件 |
| R2 Secret Access Key | 上传和下载 R2 文件 |

第一次添加的 Cloudflare 账号会作为主账号。后续你也可以继续添加副账号，把不同游戏放到不同 R2 Bucket 中。

### 可选 API Key

| API Key | 作用 |
| --- | --- |
| RAWG API Key | 搜索游戏资料、简介、评分、发行时间和标签 |
| SteamGridDB API Key | 搜索游戏竖版封面 |

这两个 API Key 不是同步存档必需的，只影响游戏资料和封面搜索。

## 添加游戏

在主界面点击“添加游戏”，通常只需要确认这些内容：

| 字段 | 怎么填 |
| --- | --- |
| 游戏名称 | 在 GameSync 中显示的名字 |
| 启动文件 | 游戏的 `.exe` 或启动入口 |
| 存档目录 | 真正需要同步的存档文件夹 |
| 封面 | 可以选本地图片，也可以用 RAWG / SteamGridDB 搜索 |
| 同步账号 | 此游戏使用哪个 Cloudflare 存储账号 |
| 冲突策略 | 手动选择、本地优先或云端优先 |

保存后，可以先点一次“手动同步”，让云端生成第一份同步记录。之后再在另一台电脑上添加同一个游戏并同步，就能拉取云端存档。

## 日常使用

### 手动同步

进入游戏卡片或右键菜单，点击“立即同步”。GameSync 会扫描本地存档目录，和云端记录对比后自动上传或下载变化。

### 启动前同步

如果开启启动前同步，GameSync 会在启动游戏前先检查云端状态：

| 检查结果 | 会发生什么 |
| --- | --- |
| 只有本地变化 | 上传本地存档到云端 |
| 只有云端变化 | 下载云端存档到本地 |
| 本地和云端一致 | 直接启动游戏 |
| 两边都改过 | 提示冲突，等待你选择 |

### 备份存档

打开游戏详情里的“存档备份”，可以创建新备份、查看已有备份，或恢复到某个备份版本。恢复前会覆盖当前本地存档，请确认当前进度是否还需要保留。

### 更新程序

进入“设置 -> 软件更新”，点击“检查更新”。如果有新版本，下载后点击“更新并重启”即可。

## 数据安全

GameSync 会尽量避免把敏感内容暴露到不该出现的位置：

- 本地 `state.json` 中的 Cloudflare Token、R2 密钥、RAWG Key、SteamGridDB Key 会进行本机保护。
- 导出的配置备份不会包含明文密钥。
- 云端目录不会保存 Cloudflare Token、R2 Secret、本地安装路径或本地封面缓存路径。
- 存档备份恢复前会校验文件哈希，并阻止压缩包内路径逃逸。
- 同步下载失败时，会尝试回滚本地已替换或已删除的文件。

仍然建议你妥善保管 Cloudflare API Token、R2 Access Key、恢复密码和配置备份文件。

## 常见问题

### 必须使用 Cloudflare 吗？

目前是的。GameSync 的云端目录使用 Cloudflare D1，存档文件使用 Cloudflare R2。

### 可以只在本机使用吗？

可以。你可以把它当作游戏库和本地备份工具使用；但云端同步、跨设备恢复和自动同步需要 Cloudflare 配置完整。

### 为什么提示同步冲突？

通常是因为两台设备都在上一次成功同步之后改过同一个游戏的存档。此时请选择你想保留的一侧：本地代表当前电脑，云端代表远端记录。

### 为什么检查更新显示“未配置更新源”？

这通常说明你运行的不是完整正式发布包，或只单独复制了主程序文件。请从 GitHub Releases 下载完整压缩包，并保留解压后的目录结构。

### RAWG 或 SteamGridDB 搜索失败怎么办？

先确认你在“设置 -> 同步偏好”中填写了对应 API Key。存档同步不依赖这两个服务，即使不填写也可以正常同步和备份。

### 我应该同步哪个目录？

请选择游戏真正写入存档的文件夹，不要选择游戏安装目录。第一次同步前建议先打开目录确认里面确实是存档文件。

---

<p align="center">
  <sub>Made for calmer game saves, cleaner libraries, and fewer lost evenings.</sub>
</p>
