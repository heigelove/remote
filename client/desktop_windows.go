//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetUserObjectInformationW = user32.NewProc("GetUserObjectInformationW")
)

func desktopName(hDesk uintptr) string {
	if hDesk == 0 {
		return ""
	}
	const udiObjectName = 2
	var needed uint32
	buf := make([]uint16, 256)
	ret, _, _ := procGetUserObjectInformationW.Call(
		hDesk, udiObjectName,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func isSecureDesktopName(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "winlogon") || strings.Contains(n, "screensaver")
}

func isLogonUIRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return false
	}
	var mySess uint32
	_ = windows.ProcessIdToSessionId(uint32(os.Getpid()), &mySess)
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if name == "logonui.exe" {
			var sid uint32
			if err := windows.ProcessIdToSessionId(pe.ProcessID, &sid); err == nil && sid == mySess {
				return true
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return false
}

func toWin32Int(v int) uintptr {
	return uintptr(uint32(int32(v)))
}
