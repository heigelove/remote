//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func serviceImagePath(exe string) string {
	return syscall.EscapeArg(exe) + " -service"
}

func queryWindowsService(name string) (installed bool, running bool, err error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, false, err
	}
	defer windows.CloseServiceHandle(scm)
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false, false, err
	}
	h, err := windows.OpenService(scm, ptr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, false, nil
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return true, false, err
	}
	return true, st.CurrentState == windows.SERVICE_RUNNING, nil
}

func installInputService() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败(需管理员): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		s, err = m.CreateService(serviceName, exe, mgr.Config{
			DisplayName:  serviceDisplay,
			Description:  serviceDesc,
			StartType:    mgr.StartAutomatic,
			ErrorControl: mgr.ErrorNormal,
		}, "-service")
		if err != nil {
			return fmt.Errorf("创建服务失败: %w", err)
		}
		_ = s.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		}, 60)
		LogInfo("input service created: %s", serviceImagePath(exe))
	}
	defer s.Close()

	if err := applyServicePath(s, exe); err != nil {
		return err
	}
	allowUsersStartService()
	if err := restartOpenedService(s); err != nil {
		return err
	}
	LogInfo("input service installed and started: %s", serviceImagePath(exe))
	return nil
}

func applyServicePath(s *mgr.Service, exe string) error {
	cfg, err := s.Config()
	if err != nil {
		return fmt.Errorf("读取服务配置失败: %w", err)
	}
	want := serviceImagePath(exe)
	if cfg.BinaryPathName == want && cfg.StartType == mgr.StartAutomatic {
		return nil
	}
	cfg.BinaryPathName = want
	cfg.StartType = mgr.StartAutomatic
	cfg.ErrorControl = mgr.ErrorNormal
	cfg.DisplayName = serviceDisplay
	cfg.Description = serviceDesc
	if err := s.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("更新服务路径失败: %w", err)
	}
	LogInfo("service ImagePath updated to %s", want)
	return nil
}

func uninstallInputService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败(需管理员): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("服务未安装")
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	for i := 0; i < 25; i++ {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("删除服务失败: %w", err)
	}
	LogInfo("input service uninstalled")
	return nil
}

func startInputService() error {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(scm)
	ptr, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return err
	}
	h, err := windows.OpenService(scm, ptr, windows.SERVICE_START|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return err
	}
	if st.CurrentState == windows.SERVICE_RUNNING || st.CurrentState == windows.SERVICE_START_PENDING {
		return nil
	}
	if err := windows.StartService(h, 0, nil); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return nil
}

func restartInputService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("服务未安装")
	}
	defer s.Close()
	if exe, err := exePath(); err == nil {
		if abs, err := filepath.Abs(exe); err == nil {
			_ = applyServicePath(s, abs)
		}
	}
	allowUsersStartService()
	return restartOpenedService(s)
}

func restartOpenedService(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State != svc.Stopped && st.State != svc.StartPending {
		_, _ = s.Control(svc.Stop)
		for i := 0; i < 40; i++ {
			st, err = s.Query()
			if err != nil || st.State == svc.Stopped {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("启动服务失败: %w", err)
	}
	for i := 0; i < 40; i++ {
		st, err = s.Query()
		if err == nil && st.State == svc.Running {
			LogInfo("input service running")
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	st, _ = s.Query()
	return fmt.Errorf("服务未能进入运行状态 (state=%d win32=%d)", st.State, st.Win32ExitCode)
}

func allowUsersStartService() {
	sddl := "D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)(A;;CCLCSWRPWPLOCRRC;;;AU)(A;;CCLCSWRPWPLOCRRC;;;IU)"
	if err := setServiceSDDL(sddl); err != nil {
		out, err2 := exec.Command("sc.exe", "sdset", serviceName, sddl).CombinedOutput()
		if err2 != nil {
			LogWarn("set service DACL: %v / sc sdset: %v %s", err, err2, stringsTrim(out))
			return
		}
	}
	LogInfo("input service start permission granted to users")
}

func setServiceSDDL(sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(scm)
	ptr, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return err
	}
	h, err := windows.OpenService(scm, ptr, windows.WRITE_DAC|windows.READ_CONTROL)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	proc := windows.NewLazySystemDLL("advapi32.dll").NewProc("SetServiceObjectSecurity")
	r, _, callErr := proc.Call(uintptr(h), uintptr(windows.DACL_SECURITY_INFORMATION), uintptr(unsafe.Pointer(sd)))
	if r == 0 {
		return callErr
	}
	return nil
}

func stringsTrim(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

var (
	ensureSvcMu     sync.Mutex
	ensureSvcLogged atomic.Int64
)

func ensureInputServiceRunning() {
	if isInputServiceReachable() {
		return
	}
	ensureSvcMu.Lock()
	defer ensureSvcMu.Unlock()
	if isInputServiceReachable() {
		return
	}
	if err := startInputService(); err != nil {
		now := time.Now().UnixMilli()
		if now-ensureSvcLogged.Load() > 60000 {
			ensureSvcLogged.Store(now)
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				LogWarn("auto-start input service: Access is denied. 请再点一次「安装锁屏输入服务」并允许 UAC（只需一次，之后会自动启动）")
			} else {
				LogWarn("auto-start input service: %v", err)
			}
		}
		return
	}
	for i := 0; i < 20; i++ {
		if isInputServiceReachable() {
			LogInfo("input service started from agent")
			svcReady.Store(true)
			svcReadyAt.Store(time.Now().UnixMilli())
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func elevateAndRun(args string) error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(args)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	err = windows.ShellExecute(0, verb, file, params, dir, windows.SW_NORMAL)
	if err != nil {
		return fmt.Errorf("提权失败(请允许 UAC): %w", err)
	}
	return nil
}

func InstallInputServiceElevated() error {
	return elevateAndRun("-install-service")
}

func UninstallInputServiceElevated() error {
	return elevateAndRun("-uninstall-service")
}

func runInstallServiceCLI() {
	_ = os.Chdir(filepath.Dir(mustExe()))
	_, _ = initLogger()
	if err := installInputService(); err != nil {
		LogErrorErr("install service", err)
		fmt.Fprintf(os.Stderr, "安装失败: %v\n", err)
		time.Sleep(3 * time.Second)
		os.Exit(1)
	}
	fmt.Println("Remote 输入服务安装并启动成功")
	time.Sleep(2 * time.Second)
}

func runUninstallServiceCLI() {
	_ = os.Chdir(filepath.Dir(mustExe()))
	_, _ = initLogger()
	if err := uninstallInputService(); err != nil {
		LogErrorErr("uninstall service", err)
		fmt.Fprintf(os.Stderr, "卸载失败: %v\n", err)
		time.Sleep(3 * time.Second)
		os.Exit(1)
	}
	fmt.Println("Remote 输入服务已卸载")
	time.Sleep(2 * time.Second)
}

func mustExe() string {
	p, err := exePath()
	if err != nil {
		return "."
	}
	return p
}
