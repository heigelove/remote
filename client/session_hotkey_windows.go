//go:build windows

package main

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	whKeyboardLL   = 13
	wmKeyDown      = 0x0100
	wmKeyUp        = 0x0101
	wmSysKeyDown   = 0x0104
	wmSysKeyUp     = 0x0105
	wmQuit         = 0x0012
	llkhfExtended  = 0x01
	llkhfInjected  = 0x10
	vkLWin         = 0x5B
	vkRWin         = 0x5C
)

type kbdllhookstruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winPOINT struct {
	X, Y int32
}

type winMSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPOINT
}

var (
	hotkeyUser32   = syscall.NewLazyDLL("user32.dll")
	hotkeyKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSetWindowsHookExW       = hotkeyUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx     = hotkeyUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx          = hotkeyUser32.NewProc("CallNextHookEx")
	procGetMessageW             = hotkeyUser32.NewProc("GetMessageW")
	procTranslateMessage        = hotkeyUser32.NewProc("TranslateMessage")
	procDispatchMessageW        = hotkeyUser32.NewProc("DispatchMessageW")
	procGetForegroundWindow     = hotkeyUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = hotkeyUser32.NewProc("GetWindowThreadProcessId")
	procPostThreadMessageW      = hotkeyUser32.NewProc("PostThreadMessageW")
	procGetModuleHandleW        = hotkeyKernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId      = hotkeyKernel32.NewProc("GetCurrentThreadId")
)

var (
	sessionHotkeyCB   = syscall.NewCallback(sessionLowLevelKeyboard)
	sessionHotkeyHook uintptr
	sessionHotkeyTID  uint32
	sessionHotkeyCtx  context.Context
	sessionHotkeyCh   chan map[string]interface{}

	hookLWin, hookRWin bool
	hookCtrl, hookAlt, hookShift bool
	hookHeld map[uint32]bool
)

func startSessionHotkeyHook(ctx context.Context) {
	sessionHotkeyCtx = ctx
	hookHeld = map[uint32]bool{}
	sessionHotkeyCh = make(chan map[string]interface{}, 64)
	go func() {
		for payload := range sessionHotkeyCh {
			if sessionHotkeyCtx == nil {
				continue
			}
			wailsruntime.EventsEmit(sessionHotkeyCtx, "session:hotkey", payload)
		}
	}()
	go func() {
		runtime.LockOSThread()
		runSessionHotkeyLoop()
	}()
}

func stopSessionHotkeyHook() {
	if sessionHotkeyHook != 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(sessionHotkeyHook)
		sessionHotkeyHook = 0
	}
	if sessionHotkeyTID != 0 {
		_, _, _ = procPostThreadMessageW.Call(uintptr(sessionHotkeyTID), wmQuit, 0, 0)
	}
}

func runSessionHotkeyLoop() {
	mod, _, _ := procGetModuleHandleW.Call(0)
	h, _, err := procSetWindowsHookExW.Call(whKeyboardLL, sessionHotkeyCB, mod, 0)
	if h == 0 {
		LogWarn("session keyboard hook failed: %v", err)
		return
	}
	sessionHotkeyHook = h
	tid, _, _ := procGetCurrentThreadId.Call()
	sessionHotkeyTID = uint32(tid)
	LogInfo("session keyboard hook installed")
	var msg winMSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if sessionHotkeyHook != 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(sessionHotkeyHook)
		sessionHotkeyHook = 0
	}
}

func foregroundIsThisProcess() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return int(pid) == os.Getpid()
}

func sessionLowLevelKeyboard(nCode uintptr, wParam uintptr, lParam uintptr) uintptr {
	if int32(nCode) < 0 {
		r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return r
	}
	kb := (*kbdllhookstruct)(unsafe.Pointer(lParam))
	if kb.Flags&llkhfInjected != 0 {
		r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return r
	}
	down := wParam == wmKeyDown || wParam == wmSysKeyDown
	up := wParam == wmKeyUp || wParam == wmSysKeyUp
	if !down && !up {
		r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return r
	}
	vk := kb.VkCode
	switch vk {
	case 0x11, 0xA2, 0xA3:
		hookCtrl = down
	case 0x12, 0xA4, 0xA5:
		hookAlt = down
	case 0x10, 0xA0, 0xA1:
		hookShift = down
	case vkLWin:
		hookLWin = down
	case vkRWin:
		hookRWin = down
	}
	winHeld := hookLWin || hookRWin
	isWin := vk == vkLWin || vk == vkRWin
	// Session 窗口在前台时拦截全部按键：Win 组合、Ctrl+C/V、Ctrl+Space / Ctrl+Shift / Alt+Shift 等输入法切换。
	if !foregroundIsThisProcess() {
		r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
		return r
	}
	if down {
		if hookHeld[vk] {
			return 1
		}
		hookHeld[vk] = true
	} else {
		delete(hookHeld, vk)
	}
	payload := map[string]interface{}{
		"action":   map[bool]string{true: "keydown", false: "keyup"}[down],
		"vk":       int(vk),
		"ctrl":     hookCtrl,
		"alt":      hookAlt,
		"shift":    hookShift,
		"meta":     winHeld || isWin,
		"extended": kb.Flags&llkhfExtended != 0 || isWin,
	}
	if sessionHotkeyCh != nil {
		select {
		case sessionHotkeyCh <- payload:
		default:
		}
	}
	return 1
}
