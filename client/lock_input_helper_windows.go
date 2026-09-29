//go:build windows

package main

import (
	"net"
	"os"
	"runtime"
	"time"
	"unsafe"
)

var lockHelperDesk uintptr
var lockHelperLastAttach time.Time

func runLockInputHelper() {
	runtime.LockOSThread()
	if dir, err := os.Getwd(); err == nil {
		LogInfo("lock-input helper start cwd=%s", dir)
	}

	jobs := make(chan InputIPCMsg, 1024)
	ln, err := listenNamedPipe(lockInjectPipeName)
	if err != nil {
		LogErrorErr("lock-input listen", err)
		os.Exit(1)
	}
	defer ln.Close()
	LogInfo("lock-input helper listening on %s", lockInjectPipeName)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				LogWarn("lock-input accept: %v", err)
				time.Sleep(200 * time.Millisecond)
				continue
			}
			go serveLockInputConn(conn, jobs)
		}
	}()

	attachLockDesktop()
	for msg := range jobs {
		if msg.Action == "quit" {
			LogInfo("lock-input helper quit")
			return
		}
		if msg.Action != "mousemove" {
			attachLockDesktop()
		} else if time.Since(lockHelperLastAttach) > 400*time.Millisecond {
			attachLockDesktop()
		}
		applyInputMsg(msg)
	}
}

func serveLockInputConn(conn net.Conn, jobs chan<- InputIPCMsg) {
	feedInputPipe(conn, func(msg InputIPCMsg) {
		if msg.Action == "mousemove" {
			select {
			case jobs <- msg:
			default:
			}
			return
		}
		jobs <- msg
	})
}

func attachLockDesktop() {
	oldWinsta, _, _ := procGetProcessWindowStation.Call()
	hWinsta, _, _ := procOpenWindowStationW.Call(
		uintptr(unsafe.Pointer(utf16Ptr("WinSta0"))),
		0,
		winstaAllAccess,
	)
	if hWinsta != 0 {
		_, _, _ = procSetProcessWindowStation.Call(hWinsta)
		if oldWinsta != 0 && oldWinsta != hWinsta {
			_, _, _ = procCloseWindowStation.Call(oldWinsta)
		}
	}

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
		LogWarn("lock-input: open desktop failed")
		return
	}
	name := desktopName(hDesk)
	if lockHelperDesk != 0 && desktopName(lockHelperDesk) == name {
		_, _, _ = procCloseDesktop.Call(hDesk)
		lockHelperLastAttach = time.Now()
		return
	}
	if r, _, err := procSetThreadDesktop.Call(hDesk); r == 0 {
		LogWarn("lock-input SetThreadDesktop %s: %v", name, err)
		_, _, _ = procCloseDesktop.Call(hDesk)
		return
	}
	if lockHelperDesk != 0 && lockHelperDesk != hDesk {
		_, _, _ = procCloseDesktop.Call(lockHelperDesk)
	}
	lockHelperDesk = hDesk
	lockHelperLastAttach = time.Now()
	LogInfo("lock-input attached desktop=%s", name)
}
