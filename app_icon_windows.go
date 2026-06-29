//go:build windows

package main

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

const (
	mainWindowClassName = "GameSyncWindow"

	wmSetIcon  = 0x0080
	iconSmall  = 0
	iconBig    = 1
	iconSmall2 = 2

	gclpHIcon   = -14
	gclpHIconSm = -34
)

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procSetClassLongPtrW         = user32.NewProc("SetClassLongPtrW")
	procGetCurrentProcessId      = kernel32.NewProc("GetCurrentProcessId")
)

type mainWindowEnumState struct {
	processID uint32
	handles   []windows.Handle
}

func (a *App) applyWindowIcon(ctx context.Context) {
	cleanup, updated, err := applyMainWindowIcon(trayIconPNG)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		wailsruntime.LogErrorf(ctx, "apply window icon failed: %v", err)
		return
	}
	if updated == 0 {
		if cleanup != nil {
			cleanup()
		}
		return
	}

	a.windowIconMu.Lock()
	previousCleanup := a.windowIconCleanup
	a.windowIconCleanup = cleanup
	a.windowIconMu.Unlock()

	if previousCleanup != nil {
		previousCleanup()
	}
}

func (a *App) releaseWindowIcon() {
	a.windowIconMu.Lock()
	cleanup := a.windowIconCleanup
	a.windowIconCleanup = nil
	a.windowIconMu.Unlock()

	if cleanup != nil {
		cleanup()
	}
}

func applyMainWindowIcon(iconData []byte) (func(), int, error) {
	if len(iconData) == 0 {
		return nil, 0, fmt.Errorf("embedded app icon is empty")
	}

	bigIcon, err := loadTrayIcon("", iconData, 256, 256)
	if err != nil {
		return nil, 0, fmt.Errorf("create big window icon: %w", err)
	}
	smallIcon, err := loadTrayIcon("", iconData, 32, 32)
	if err != nil {
		destroyIconHandles(bigIcon)
		return nil, 0, fmt.Errorf("create small window icon: %w", err)
	}
	small2Icon, err := loadTrayIcon("", iconData, 16, 16)
	if err != nil {
		destroyIconHandles(bigIcon, smallIcon)
		return nil, 0, fmt.Errorf("create small2 window icon: %w", err)
	}

	cleanup := func() {
		destroyIconHandles(bigIcon, smallIcon, small2Icon)
	}

	handles, err := findMainWindowHandles()
	if err != nil {
		return cleanup, 0, err
	}
	for _, hwnd := range handles {
		_, _, _ = procSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconBig, uintptr(bigIcon))
		_, _, _ = procSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconSmall, uintptr(smallIcon))
		_, _, _ = procSendMessageW.Call(uintptr(hwnd), wmSetIcon, iconSmall2, uintptr(small2Icon))
		_, _, _ = procSetClassLongPtrW.Call(uintptr(hwnd), signedIndex(gclpHIcon), uintptr(bigIcon))
		_, _, _ = procSetClassLongPtrW.Call(uintptr(hwnd), signedIndex(gclpHIconSm), uintptr(smallIcon))
	}

	return cleanup, len(handles), nil
}

func findMainWindowHandles() ([]windows.Handle, error) {
	currentProcessID, _, _ := procGetCurrentProcessId.Call()
	state := &mainWindowEnumState{processID: uint32(currentProcessID)}
	callback := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		enumState := (*mainWindowEnumState)(unsafe.Pointer(lParam))
		var windowProcessID uint32
		_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&windowProcessID)))
		if windowProcessID != enumState.processID {
			return 1
		}

		handle := windows.Handle(hwnd)
		className := getWindowClassName(handle)
		title := getWindowTitle(handle)
		if className == mainWindowClassName || (strings.EqualFold(title, "GameSync") && className != "GameSyncTrayWindow") {
			enumState.handles = append(enumState.handles, handle)
		}
		return 1
	})

	ret, _, err := procEnumWindows.Call(callback, uintptr(unsafe.Pointer(state)))
	if ret == 0 {
		return nil, fmt.Errorf("enum windows: %w", err)
	}
	return state.handles, nil
}

func getWindowClassName(hwnd windows.Handle) string {
	var buffer [256]uint16
	ret, _, _ := procGetClassNameW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	return windows.UTF16ToString(buffer[:ret])
}

func getWindowTitle(hwnd windows.Handle) string {
	var buffer [256]uint16
	ret, _, _ := procGetWindowTextW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	return windows.UTF16ToString(buffer[:ret])
}

func destroyIconHandles(handles ...windows.Handle) {
	for _, handle := range handles {
		if handle != 0 {
			_, _, _ = procDestroyIcon.Call(uintptr(handle))
		}
	}
}

func signedIndex(value int) uintptr {
	return uintptr(int64(value))
}
