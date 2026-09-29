//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow           = 0x08000000
	createBreakawayFromJob   = 0x01000000
	createNewProcessGroup    = 0x00000200
	createUnicodeEnvironment = 0x00000400
)

var (
	modKernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	modUserenv                       = windows.NewLazySystemDLL("userenv.dll")
	procWTSGetActiveConsoleSessionId = modKernel32.NewProc("WTSGetActiveConsoleSessionId")
	procCreateEnvironmentBlock       = modUserenv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock      = modUserenv.NewProc("DestroyEnvironmentBlock")
	lockHelperPID                    atomic.Uint32
)

func keepLockInputHelper(stop <-chan struct{}) {
	_ = sendInputToLockHelper(InputIPCMsg{Action: "quit"})
	time.Sleep(200 * time.Millisecond)
	killProcessesNamed("remote-lockinput.exe")
	time.Sleep(200 * time.Millisecond)

	spawnLockInputHelper()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !isLockHelperReachable() {
				LogWarn("lockhelper unreachable, respawning")
				spawnLockInputHelper()
			}
		}
	}
}

func stopLockInputHelper() {
	_ = sendInputToLockHelper(InputIPCMsg{Action: "quit"})
	time.Sleep(250 * time.Millisecond)
	pid := lockHelperPID.Load()
	if pid == 0 {
		return
	}
	terminatePID(pid)
	lockHelperPID.Store(0)
}

func spawnLockInputHelper() {
	if isLockHelperReachable() {
		return
	}
	helperExe, err := lockHelperExePath()
	if err != nil {
		LogErrorErr("lockhelper exe", err)
		return
	}
	sessionID := activeConsoleSessionID()
	if sessionID == 0 || sessionID == 0xFFFFFFFF {
		if pid, err := findAnyInteractiveWinlogon(); err == nil {
			_ = windows.ProcessIdToSessionId(pid, &sessionID)
		}
	}
	if sessionID == 0 || sessionID == 0xFFFFFFFF {
		LogError("lockhelper: no interactive session")
		return
	}
	token, src, err := interactiveInjectToken(sessionID)
	if err != nil {
		LogErrorErr("lockhelper token", err)
		return
	}
	defer token.Close()

	pid, err := createProcessOnWinlogon(token, helperExe)
	if err != nil {
		LogErrorErr("lockhelper CreateProcessAsUser", err)
		return
	}
	lockHelperPID.Store(pid)
	LogInfo("lockhelper started pid=%d session=%d token=%s exe=%s", pid, sessionID, src, helperExe)

	for i := 0; i < 25; i++ {
		if isLockHelperReachable() {
			LogInfo("lockhelper pipe ready")
			return
		}
		time.Sleep(120 * time.Millisecond)
	}
	LogWarn("lockhelper pid=%d started but pipe %s not ready", pid, lockInjectPipeName)
}

func lockHelperExePath() (string, error) {
	self, err := exePath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(self)
	candidates := []string{
		filepath.Join(dir, "remote-lockinput.exe"),
		filepath.Join(dir, "lockhelper.exe"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return self, nil
}

func activeConsoleSessionID() uint32 {
	r, _, _ := procWTSGetActiveConsoleSessionId.Call()
	return uint32(r)
}

func interactiveInjectToken(sessionID uint32) (windows.Token, string, error) {
	pid, err := findWinlogonPID(sessionID)
	if err != nil {
		return 0, "", err
	}
	tok, err := duplicateProcessToken(pid)
	if err != nil {
		return 0, "", err
	}
	return tok, "winlogon", nil
}

func enableInjectPrivileges() {
	names := []string{
		"SeAssignPrimaryTokenPrivilege",
		"SeIncreaseQuotaPrivilege",
		"SeTcbPrivilege",
		"SeDebugPrivilege",
		"SeImpersonatePrivilege",
	}
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		LogWarn("OpenProcessToken privileges: %v", err)
		return
	}
	defer tok.Close()
	for _, name := range names {
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid); err != nil {
			continue
		}
		var tp windows.Tokenprivileges
		tp.PrivilegeCount = 1
		tp.Privileges[0].Luid = luid
		tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_ENABLED
		if err := windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil); err != nil {
			LogWarn("enable privilege %s: %v", name, err)
		}
	}
}

func findWinlogonPID(sessionID uint32) (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return 0, err
	}
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if name == "winlogon.exe" {
			var sid uint32
			if err := windows.ProcessIdToSessionId(pe.ProcessID, &sid); err == nil && sid == sessionID {
				return pe.ProcessID, nil
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	if sessionID != 0 {
		return findAnyInteractiveWinlogon()
	}
	return 0, fmt.Errorf("winlogon.exe not found in session %d", sessionID)
}

func findAnyInteractiveWinlogon() (uint32, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return 0, err
	}
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if name == "winlogon.exe" {
			var sid uint32
			if err := windows.ProcessIdToSessionId(pe.ProcessID, &sid); err == nil && sid != 0 {
				return pe.ProcessID, nil
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return 0, fmt.Errorf("no interactive winlogon.exe")
}

func duplicateProcessToken(pid uint32) (windows.Token, error) {
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		proc, err = windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, pid)
		if err != nil {
			return 0, fmt.Errorf("OpenProcess %d: %w", pid, err)
		}
	}
	defer windows.CloseHandle(proc)

	var tok windows.Token
	access := uint32(windows.TOKEN_DUPLICATE | windows.TOKEN_QUERY | windows.TOKEN_ASSIGN_PRIMARY |
		windows.TOKEN_ADJUST_DEFAULT | windows.TOKEN_ADJUST_SESSIONID)
	if err := windows.OpenProcessToken(proc, access, &tok); err != nil {
		return 0, fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer tok.Close()

	var primary windows.Token
	err = windows.DuplicateTokenEx(
		tok,
		windows.MAXIMUM_ALLOWED,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&primary,
	)
	if err != nil {
		return 0, fmt.Errorf("DuplicateTokenEx: %w", err)
	}
	return primary, nil
}

func createProcessOnWinlogon(token windows.Token, exe string) (uint32, error) {
	app, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return 0, err
	}
	desk, err := windows.UTF16PtrFromString(`winsta0\Winlogon`)
	if err != nil {
		return 0, err
	}
	cmd := fmt.Sprintf(`"%s"`, exe)
	if strings.EqualFold(filepath.Base(exe), "remote-client.exe") {
		cmd = fmt.Sprintf(`"%s" -lock-input`, exe)
	}
	cmdSlice, err := windows.UTF16FromString(cmd)
	if err != nil {
		return 0, err
	}

	var env *uint16
	if r, _, _ := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&env)), uintptr(token), 0); r == 0 {
		env = nil
	} else {
		defer procDestroyEnvironmentBlock.Call(uintptr(unsafe.Pointer(env)))
	}

	flags := uint32(createBreakawayFromJob | createNoWindow | createNewProcessGroup)
	if env != nil {
		flags |= createUnicodeEnvironment
	}

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop = desk
	si.Flags = windows.STARTF_USESHOWWINDOW
	si.ShowWindow = windows.SW_HIDE
	var pi windows.ProcessInformation
	err = windows.CreateProcessAsUser(token, app, &cmdSlice[0], nil, nil, false, flags, env, dir, &si, &pi)
	if err != nil {
		si.Desktop = desk
		err = windows.CreateProcessAsUser(token, app, &cmdSlice[0], nil, nil, false, createBreakawayFromJob, env, dir, &si, &pi)
	}
	if err != nil {
		return 0, err
	}
	_ = windows.CloseHandle(pi.Thread)
	_ = windows.CloseHandle(pi.Process)
	LogInfo("lockhelper CreateProcessAsUser cmd=%s pid=%d", cmd, pi.ProcessId)
	return pi.ProcessId, nil
}

func terminatePID(pid uint32) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 0)
}

func killProcessesNamed(exeName string) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := windows.Process32First(snap, &pe); err != nil {
		return
	}
	want := strings.ToLower(exeName)
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		if name == want && pe.ProcessID != 0 {
			LogInfo("killing leftover %s pid=%d", exeName, pe.ProcessID)
			terminatePID(pe.ProcessID)
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
}
