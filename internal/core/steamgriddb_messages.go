package core

const (
	msgSteamGridDBAPIKeyRequired      = "请先在设置页填写 SteamGridDB API Key"
	msgSteamGridDBQueryRequired       = "请输入要搜索的游戏名称"
	msgInvalidSteamGridDBGameID       = "无效的 SteamGridDB 游戏 ID"
	msgBuildSteamGridDBRequestFailed  = "构造 SteamGridDB 请求失败: %w"
	msgVisitSteamGridDBFailed         = "访问 SteamGridDB 失败: %w"
	msgReadSteamGridDBResponseFailed  = "读取 SteamGridDB 响应失败: %w"
	msgSteamGridDBRequestFailed       = "SteamGridDB 请求失败（%d）: %s"
	msgParseSteamGridDBResponseFailed = "解析 SteamGridDB 响应失败: %w"
)
