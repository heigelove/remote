//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "Remote"

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(exe)
}

func IsAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func SetAutoStart(enable bool) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf(`"%s"`, exe)

	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enable {
		return k.SetStringValue(runValueName, cmd)
	}
	_ = k.DeleteValue(runValueName)
	return nil
}

// isWorkstationLocked reports whether the interactive input desktop is Winlogon.
//
// IMPORTANT: Never treat "OpenInputDesktop failed with weak access" as locked.
// A false positive routes clicks into the SYSTEM service, which often injects on
// the wrong session/desktop — mouse clicks then appear to do nothing.
func isWorkstationLocked() bool {
	user32 := syscall.NewLazyDLL("user32.dll")
	openInputDesktop := user32.NewProc("OpenInputDesktop")
	closeDesktop := user32.NewProc("CloseDesktop")
	getUserObjectInformation := user32.NewProc("GetUserObjectInformationW")
	openDesktop := user32.NewProc("OpenDesktopW")

	const desktopAll = 0x01FF
	h, _, _ := openInputDesktop.Call(0, 0, desktopAll)
	if h == 0 {
		// Input desktop inaccessible. If Default still opens, session is likely locked.
		name, _ := syscall.UTF16PtrFromString("Default")
		hDef, _, _ := openDesktop.Call(uintptr(unsafe.Pointer(name)), 0, 0, desktopAll)
		if hDef != 0 {
			_, _, _ = closeDesktop.Call(hDef)
			return true
		}
		return false
	}
	defer closeDesktop.Call(h)

	const udiObjectName = 2
	var needed uint32
	buf := make([]uint16, 256)
	ret, _, _ := getUserObjectInformation.Call(
		h, udiObjectName,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)*2),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ret == 0 {
		return false
	}
	desk := strings.ToLower(syscall.UTF16ToString(buf))
	return strings.Contains(desk, "winlogon") || strings.Contains(desk, "screensaver")
}
