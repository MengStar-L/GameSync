package core

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// ProcessMonitor 帮助管理并追踪由于各种原因（如被平台调起）而断开直接父子级关系的进程
type ProcessMonitor struct{}

func NewProcessMonitor() *ProcessMonitor {
	return &ProcessMonitor{}
}

// LaunchAndMonitor 尝试启动并追踪游戏。
// 针对 Steam 游戏等启动器，直接运行的 exe 会立即退出，这里采用循环侦听其所处目录的后代进程。
func (pm *ProcessMonitor) LaunchAndMonitor(ctx context.Context, installPath string, onStart func(int32), onEnd func(time.Duration)) error {
	if strings.TrimSpace(installPath) == "" {
		return fmt.Errorf(msgInvalidLaunchPath)
	}

	startTime := time.Now()

	// 启动游戏
	cmd := exec.Command("cmd", "/C", "start", "", installPath)
	err := cmd.Start()
	if err != nil {
		return fmt.Errorf(msgLaunchFailed, err)
	}

	gameDir := filepath.Dir(installPath)
	if gameDir == "." {
		gameDir = installPath
	}
	gameDir = strings.ToLower(filepath.Clean(gameDir))

	go func() {
		defer func() {
			duration := time.Since(startTime)
			if onEnd != nil {
				onEnd(duration)
			}
		}()

		var trackedPid int32 = 0

		// 阶段一：侦听寻找游戏进程（最长等待 60 秒）
		timeout := time.After(60 * time.Second)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

	SearchLoop:
		for {
			select {
			case <-ctx.Done():
				return
			case <-timeout:
				return // 超时未找到进程，视为单次速刷（直接进入 end 回调）
			case <-ticker.C:
				pids, err := process.Pids()
				if err != nil {
					continue
				}

				for _, pid := range pids {
					p, err := process.NewProcess(pid)
					if err != nil {
						continue
					}
					exe, err := p.Exe()
					if err != nil || exe == "" {
						continue
					}

					// 如果进程路径存在于游戏安装目录下，则判定为实际游戏进程
					if strings.Contains(strings.ToLower(filepath.Clean(exe)), gameDir) {
						trackedPid = pid
						if onStart != nil {
							onStart(trackedPid)
						}
						break SearchLoop
					}
				}
			}
		}

		// 阶段二：监视判定为游戏的进程，直至其退出
		monitorTicker := time.NewTicker(5 * time.Second)
		defer monitorTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-monitorTicker.C:
				exists, err := process.PidExists(trackedPid)
				if err != nil || !exists {
					// 进程已退出
					return
				}
			}
		}
	}()

	return nil
}
