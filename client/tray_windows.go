//go:build windows

package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	trayCallbackMsg = 0x8001
	trayQuitMsg     = 0x8002
	trayCmdShow     = 1001
	trayCmdExit     = 1002
	wmRButtonUp     = 0x0205
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmContextMenu   = 0x007B
	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmCommand       = 0x0111

	nimAdd    = 0x00000000
	nimDelete = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmRightAlign  = 0x0008
	idiApplication = 32512
)

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Timeout         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        [16]byte
	BalloonIcon     uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

var (
	procShellNotifyIconW = syscall.NewLazyDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	procExtractIconExW   = syscall.NewLazyDLL("shell32.dll").NewProc("ExtractIconExW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procSetForegroundWnd = user32.NewProc("SetForegroundWindow")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
)

var (
	trayApp     *App
	trayHWND    atomic.Uintptr
	trayIcon    uintptr
	trayOwnIcon bool
	trayClass   = syscall.NewCallback(trayWndProc)
	trayRunning atomic.Bool
)

func startTray(app *App) {
	if app == nil || isSessionWindow() || !trayRunning.CompareAndSwap(false, true) {
		return
	}
	trayApp = app
	go trayLoop()
}

func stopTray() {
	h := trayHWND.Load()
	if h != 0 {
		_, _, _ = procPostMessageW.Call(h, trayQuitMsg, 0, 0)
	}
}

func trayLoop() {
	inst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("RemoteTrayWnd")
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   trayClass,
		Instance:  inst,
		ClassName: className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		LogWarn("tray RegisterClassEx: %v", err)
		trayRunning.Store(false)
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, inst, 0)
	if hwnd == 0 {
		LogWarn("tray CreateWindowEx: %v", err)
		trayRunning.Store(false)
		return
	}
	trayHWND.Store(hwnd)
	trayIcon = loadTrayIcon()
	if err := addTrayIcon(hwnd, trayIcon); err != nil {
		LogWarn("tray add icon: %v", err)
	} else {
		LogInfo("system tray ready")
	}

	var msg winMSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	trayRunning.Store(false)
}

func loadTrayIcon() uintptr {
	exe, err := os.Executable()
	if err == nil && exe != "" {
		p, _ := syscall.UTF16PtrFromString(exe)
		var small uintptr
		n, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(p)), 0, 0, uintptr(unsafe.Pointer(&small)), 1)
		if n != 0 && small != 0 {
			trayOwnIcon = true
			return small
		}
	}
	trayOwnIcon = false
	h, _, _ := procLoadIconW.Call(0, uintptr(idiApplication))
	return h
}

func addTrayIcon(hwnd, icon uintptr) error {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = hwnd
	nid.ID = 1
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = trayCallbackMsg
	nid.Icon = icon
	copyUTF16Tip(&nid.Tip, "Remote")
	r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		return err
	}
	return nil
}

func deleteTrayIcon(hwnd uintptr) {
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Wnd = hwnd
	nid.ID = 1
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if trayIcon != 0 && trayOwnIcon {
		_, _, _ = procDestroyIcon.Call(trayIcon)
		trayIcon = 0
		trayOwnIcon = false
	}
}

func copyUTF16Tip(dst *[128]uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	n := len(u)
	if n > 127 {
		n = 127
		u[126] = 0
	}
	copy(dst[:], u[:n])
}

func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case trayCallbackMsg:
		switch lParam {
		case wmLButtonUp, wmLButtonDblClk:
			showMainWindow()
		case wmRButtonUp, wmContextMenu:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		switch uint16(wParam) {
		case trayCmdShow:
			showMainWindow()
		case trayCmdExit:
			exitFromTray()
		}
		return 0
	case trayQuitMsg:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		deleteTrayIcon(hwnd)
		trayHWND.Store(0)
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	showText, _ := syscall.UTF16PtrFromString("打开主窗口")
	exitText, _ := syscall.UTF16PtrFromString("退出")
	_, _, _ = procAppendMenuW.Call(menu, mfString, trayCmdShow, uintptr(unsafe.Pointer(showText)))
	_, _, _ = procAppendMenuW.Call(menu, mfString, trayCmdExit, uintptr(unsafe.Pointer(exitText)))
	var pt winPOINT
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWnd.Call(hwnd)
	_, _, _ = procTrackPopupMenu.Call(menu, tpmRightButton|tpmRightAlign|tpmBottomAlign, uintptr(int32(pt.X)), uintptr(int32(pt.Y)), 0, hwnd, 0)
	_, _, _ = procPostMessageW.Call(hwnd, wmNull, 0, 0)
}

func showMainWindow() {
	app := trayApp
	if app == nil || app.ctx == nil {
		return
	}
	wailsruntime.WindowShow(app.ctx)
	wailsruntime.WindowUnminimise(app.ctx)
}

func exitFromTray() {
	app := trayApp
	if app == nil {
		return
	}
	app.allowQuit.Store(true)
	if app.ctx != nil {
		wailsruntime.Quit(app.ctx)
	}
}
