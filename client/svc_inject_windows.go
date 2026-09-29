//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	procOpenWindowStationW      = user32.NewProc("OpenWindowStationW")
	procSetProcessWindowStation = user32.NewProc("SetProcessWindowStation")
	procCloseWindowStation      = user32.NewProc("CloseWindowStation")
	procOpenDesktopW            = user32.NewProc("OpenDesktopW")
	procGetProcessWindowStation = user32.NewProc("GetProcessWindowStation")
)

const (
	winstaAllAccess  = 0x037F
	desktopAllAccess = 0x01FF
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// withSecureDesktop attaches this thread to the interactive WinSta0 input desktop
// (Winlogon when locked). Intended for LocalSystem service.
func withSecureDesktop(fn func()) {
	oldWinsta, _, _ := procGetProcessWindowStation.Call()
	hWinsta, _, _ := procOpenWindowStationW.Call(
		uintptr(unsafe.Pointer(utf16Ptr("WinSta0"))),
		0,
		winstaAllAccess,
	)
	if hWinsta != 0 {
		_, _, _ = procSetProcessWindowStation.Call(hWinsta)
		defer func() {
			if oldWinsta != 0 {
				_, _, _ = procSetProcessWindowStation.Call(oldWinsta)
			}
			_, _, _ = procCloseWindowStation.Call(hWinsta)
		}()
	}

	hThread, _, _ := procGetCurrentThread.Call()
	oldDesk, _, _ := procGetThreadDesktop.Call(hThread)

	hDesk, _, _ := procOpenInputDesktop.Call(0, 0, desktopAllAccess)
	if hDesk == 0 {
		hDesk, _, _ = procOpenDesktopW.Call(
			uintptr(unsafe.Pointer(utf16Ptr("Winlogon"))),
			0, 0, desktopAllAccess,
		)
	}
	if hDesk == 0 {
		hDesk, _, _ = procOpenDesktopW.Call(
			uintptr(unsafe.Pointer(utf16Ptr("Default"))),
			0, 0, desktopAllAccess,
		)
	}
	if hDesk == 0 {
		LogWarn("secure desktop: open failed, injecting on current desktop")
		fn()
		return
	}
	if r, _, _ := procSetThreadDesktop.Call(hDesk); r == 0 {
		LogWarn("secure desktop: SetThreadDesktop failed")
		_, _, _ = procCloseDesktop.Call(hDesk)
		fn()
		return
	}
	defer func() {
		if oldDesk != 0 {
			_, _, _ = procSetThreadDesktop.Call(oldDesk)
		}
		_, _, _ = procCloseDesktop.Call(hDesk)
	}()
	fn()
}

func injectOnSecureDesktop(msg InputIPCMsg) {
	withSecureDesktop(func() {
		applyInputMsg(msg)
	})
}
