//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

type trayController interface {
	Close()
}

type windowsTray struct {
	ctx      context.Context
	iconPath string
	iconData []byte
	hwnd     windows.Handle
	menu     windows.Handle
	icon     windows.Handle
	ready    chan error
	closed   sync.Once
}

var (
	trayWindowClassName = syscall.StringToUTF16Ptr("GameSyncTrayWindow")
	trayWindowProcPtr   = syscall.NewCallback(trayWindowProc)
	trayInstances       sync.Map

	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	procDestroyIcon         = user32.NewProc("DestroyIcon")

	procCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap     = gdi32.NewProc("CreateBitmap")
	procDeleteObject     = gdi32.NewProc("DeleteObject")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	registerTrayClassOnce sync.Once
	registerTrayClassErr  error
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmApp           = 0x8000
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmNull          = 0x0000

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	cwUseDefault = 0x80000000

	idcArrow = 32512

	wsOverlapped = 0x00000000

	mfString    = 0x0000
	mfSeparator = 0x0800

	tpmLeftAlign   = 0x0000
	tpmBottomAlign = 0x0020
	tpmRightButton = 0x0002

	biRGB        = 0
	dibRGBColors = 0

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifShowTip = 0x00000080

	trayCallbackMessage = wmApp + 1
	trayMenuShow        = 1001
	trayMenuHide        = 1002
	trayMenuQuit        = 1003
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  windows.Handle
	HbmColor windows.Handle
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

func newWindowsTray(ctx context.Context, iconPath string, iconData []byte) (*windowsTray, error) {
	tray := &windowsTray{
		ctx:      ctx,
		iconPath: iconPath,
		iconData: append([]byte(nil), iconData...),
		ready:    make(chan error, 1),
	}
	go tray.run()
	if err := <-tray.ready; err != nil {
		return nil, err
	}
	return tray, nil
}

func (t *windowsTray) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := registerTrayWindowClass(); err != nil {
		t.ready <- err
		return
	}

	instance, err := getCurrentModuleHandle()
	if err != nil {
		t.ready <- fmt.Errorf("get module handle: %w", err)
		return
	}

	// 按系统托盘的真实尺寸（DPI 感知）生成图标，避免 Shell 再次缩放造成模糊
	icon, err := loadTrayIcon(t.iconPath, t.iconData, systemMetric(smCxSmIcon, 16), systemMetric(smCySmIcon, 16))
	if err != nil {
		t.ready <- err
		return
	}
	t.icon = icon

	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(trayWindowClassName)),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("GameSyncTray"))),
		wsOverlapped,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		cwUseDefault,
		0,
		0,
		uintptr(instance),
		0,
	)
	if hwnd == 0 {
		t.ready <- fmt.Errorf("create tray window: %w", callErr)
		return
	}
	t.hwnd = windows.Handle(hwnd)
	trayInstances.Store(t.hwnd, t)

	menu, _, callErr := procCreatePopupMenu.Call()
	if menu == 0 {
		trayInstances.Delete(t.hwnd)
		_, _, _ = procDestroyWindow.Call(hwnd)
		t.ready <- fmt.Errorf("create tray menu: %w", callErr)
		return
	}
	t.menu = windows.Handle(menu)
	t.buildMenu()

	if err := t.addNotifyIcon(); err != nil {
		trayInstances.Delete(t.hwnd)
		_, _, _ = procDestroyMenu.Call(menu)
		_, _, _ = procDestroyWindow.Call(hwnd)
		t.ready <- err
		return
	}

	t.ready <- nil

	var message msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			return
		case 0:
			return
		default:
			_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
			_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
		}
	}
}

func (t *windowsTray) Close() {
	t.closed.Do(func() {
		if t.hwnd != 0 {
			_, _, _ = procPostMessageW.Call(uintptr(t.hwnd), wmClose, 0, 0)
		}
	})
}

func (t *windowsTray) buildMenu() {
	_, _, _ = procAppendMenuW.Call(uintptr(t.menu), mfString, trayMenuShow, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("显示主窗口"))))
	_, _, _ = procAppendMenuW.Call(uintptr(t.menu), mfString, trayMenuHide, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("隐藏到托盘"))))
	_, _, _ = procAppendMenuW.Call(uintptr(t.menu), mfSeparator, 0, 0)
	_, _, _ = procAppendMenuW.Call(uintptr(t.menu), mfString, trayMenuQuit, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("退出程序"))))
}

func (t *windowsTray) addNotifyIcon() error {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = 1
	nid.UFlags = nifMessage | nifIcon | nifTip | nifShowTip
	nid.UCallbackMessage = trayCallbackMessage
	nid.HIcon = t.icon
	copy(nid.SzTip[:], syscall.StringToUTF16("GameSync"))

	ret, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		return fmt.Errorf("add tray icon: %w", err)
	}
	return nil
}

func (t *windowsTray) removeNotifyIcon() {
	if t.hwnd == 0 {
		return
	}
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = 1
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

func (t *windowsTray) wndProc(hwnd windows.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case trayCallbackMessage:
		switch uint32(lParam) {
		case wmLButtonUp:
			go wailsruntime.WindowShow(t.ctx)
			return 0
		case wmLButtonDblClk:
			go wailsruntime.WindowShow(t.ctx)
			return 0
		case wmRButtonUp:
			t.showContextMenu(hwnd)
			return 0
		}
	case wmCommand:
		switch loword(uint32(wParam)) {
		case trayMenuShow:
			go wailsruntime.WindowShow(t.ctx)
		case trayMenuHide:
			go wailsruntime.WindowHide(t.ctx)
		case trayMenuQuit:
			go wailsruntime.Quit(t.ctx)
		}
		return 0
	case wmClose:
		_, _, _ = procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		t.removeNotifyIcon()
		if t.menu != 0 {
			_, _, _ = procDestroyMenu.Call(uintptr(t.menu))
			t.menu = 0
		}
		if t.icon != 0 {
			_, _, _ = procDestroyIcon.Call(uintptr(t.icon))
			t.icon = 0
		}
		trayInstances.Delete(hwnd)
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (t *windowsTray) showContextMenu(hwnd windows.Handle) {
	var cursor point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	_, _, _ = procSetForegroundWindow.Call(uintptr(hwnd))
	_, _, _ = procTrackPopupMenu.Call(
		uintptr(t.menu),
		tpmLeftAlign|tpmBottomAlign|tpmRightButton,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		0,
		uintptr(hwnd),
		0,
	)
	_, _, _ = procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
}

func trayWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if tray, ok := trayInstances.Load(windows.Handle(hwnd)); ok {
		return tray.(*windowsTray).wndProc(windows.Handle(hwnd), msg, wParam, lParam)
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func registerTrayWindowClass() error {
	registerTrayClassOnce.Do(func() {
		instance, err := getCurrentModuleHandle()
		if err != nil {
			registerTrayClassErr = err
			return
		}

		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		class := wndClassEx{
			Size:      uint32(unsafe.Sizeof(wndClassEx{})),
			Style:     csHRedraw | csVRedraw,
			WndProc:   trayWindowProcPtr,
			Instance:  instance,
			Cursor:    windows.Handle(cursor),
			ClassName: trayWindowClassName,
		}

		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 && callErr != windows.ERROR_CLASS_ALREADY_EXISTS {
			registerTrayClassErr = fmt.Errorf("register tray class: %w", callErr)
		}
	})
	return registerTrayClassErr
}

func loadTrayIcon(path string, data []byte, width, height int) (windows.Handle, error) {
	src, err := decodeTrayIcon(path, data)
	if err != nil {
		return 0, err
	}
	scaled := resizeToNRGBA(src, width, height)
	return createHIconFromNRGBA(scaled)
}

func decodeTrayIcon(path string, data []byte) (image.Image, error) {
	if len(data) > 0 {
		src, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode embedded tray icon: %w", err)
		}
		return src, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tray icon: %w", err)
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode tray icon: %w", err)
	}
	return src, nil
}

// resizeToNRGBA 用 Catmull-Rom 重采样缩放：滤波核随缩放比展开、覆盖全部源像素，
// 大倍率缩小（1383px 源图 → 16px 托盘）不丢采样，图标边缘保持锐利。
// 此前的单程双线性每目标像素只取 4 个源像素，43 倍缩小等于点采样，是图标模糊的根因。
func resizeToNRGBA(src image.Image, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	if b := src.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
		return dst
	}
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func createHIconFromNRGBA(img *image.NRGBA) (windows.Handle, error) {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()

	var pixels unsafe.Pointer
	bi := bitmapInfo{
		Header: bitmapInfoHeader{
			Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			Width:       int32(width),
			Height:      -int32(height),
			Planes:      1,
			BitCount:    32,
			Compression: biRGB,
		},
	}

	colorBitmap, _, err := procCreateDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&pixels)),
		0,
		0,
	)
	if colorBitmap == 0 {
		return 0, fmt.Errorf("create tray color bitmap: %w", err)
	}
	defer procDeleteObject.Call(colorBitmap)

	maskBitmap, _, err := procCreateBitmap.Call(uintptr(width), uintptr(height), 1, 1, 0)
	if maskBitmap == 0 {
		return 0, fmt.Errorf("create tray mask bitmap: %w", err)
	}
	defer procDeleteObject.Call(maskBitmap)

	dst := unsafe.Slice((*byte)(pixels), width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcIndex := img.PixOffset(x, y)
			dstIndex := (y*width + x) * 4
			dst[dstIndex+0] = img.Pix[srcIndex+2]
			dst[dstIndex+1] = img.Pix[srcIndex+1]
			dst[dstIndex+2] = img.Pix[srcIndex+0]
			dst[dstIndex+3] = img.Pix[srcIndex+3]
		}
	}

	iconInfo := iconInfo{
		FIcon:    1,
		HbmMask:  windows.Handle(maskBitmap),
		HbmColor: windows.Handle(colorBitmap),
	}

	icon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&iconInfo)))
	if icon == 0 {
		return 0, fmt.Errorf("create tray icon handle: %w", err)
	}

	return windows.Handle(icon), nil
}

// 系统图标度量（随 DPI 缩放）：托盘/窗口小图标与大图标的精确像素尺寸
const (
	smCxIcon   = 11
	smCyIcon   = 12
	smCxSmIcon = 49
	smCySmIcon = 50
)

var procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

func systemMetric(index, fallback int) int {
	value, _, _ := procGetSystemMetrics.Call(uintptr(index))
	if int(value) <= 0 {
		return fallback
	}
	return int(value)
}

func loword(value uint32) uint32 {
	return value & 0xffff
}

func getCurrentModuleHandle() (windows.Handle, error) {
	handle, _, err := procGetModuleHandleW.Call(0)
	if handle == 0 {
		return 0, err
	}
	return windows.Handle(handle), nil
}
