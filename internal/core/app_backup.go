package core

import (
	"encoding/json"
	"fmt"
	"time"
)

const PortableBackupFormatVersion = 2

type PortableBackup struct {
	FormatVersion int       `json:"formatVersion"`
	ExportedAt    time.Time `json:"exportedAt" ts_type:"string"`
	State         AppState  `json:"state"`
}

func EncodePortableBackup(state AppState, exportedAt time.Time) ([]byte, error) {
	portable := cloneState(state)
	clearPortableMachinePaths(&portable)
	return json.MarshalIndent(PortableBackup{
		FormatVersion: PortableBackupFormatVersion,
		ExportedAt:    exportedAt,
		State:         portable,
	}, "", "  ")
}

func DecodePortableBackup(data []byte) (AppState, error) {
	var header struct {
		FormatVersion *int            `json:"formatVersion"`
		State         json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return AppState{}, fmt.Errorf("解析导入的配置备份失败: %w", err)
	}

	var state AppState
	if header.FormatVersion == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return AppState{}, fmt.Errorf("解析旧版配置备份失败: %w", err)
		}
		return state, nil
	}
	if *header.FormatVersion != PortableBackupFormatVersion {
		return AppState{}, fmt.Errorf("不支持的配置备份版本: %d", *header.FormatVersion)
	}
	if len(header.State) == 0 || string(header.State) == "null" {
		return AppState{}, fmt.Errorf("配置备份缺少状态数据")
	}
	if err := json.Unmarshal(header.State, &state); err != nil {
		return AppState{}, fmt.Errorf("解析配置备份状态失败: %w", err)
	}
	return state, nil
}

func clearPortableMachinePaths(state *AppState) {
	if state == nil {
		return
	}
	clearPortableGamePaths(state.Games)
	state.Preferences.DefaultInstallDir = ""
	state.Preferences.DefaultSaveDir = ""
	state.Preferences.DefaultSteamInstallDir = ""
	state.Preferences.DefaultSteamSaveDir = ""
	state.Preferences.DefaultThirdInstallDir = ""
	state.Preferences.DefaultThirdSaveDir = ""
	if state.StorageMigration != nil {
		for index := range state.StorageMigration.Items {
			state.StorageMigration.Items[index].LocalPath = ""
		}
		clearPortableGamePaths(state.StorageMigration.TargetGames)
	}
}

func clearPortableGamePaths(games []Game) {
	clearGameLocalPaths(games)
	for index := range games {
		games[index].LaunchRestoreOverride = nil
	}
}
