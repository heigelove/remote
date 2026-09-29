//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
)

const serviceName = "RemotoAgentInput"
const serviceDisplay = "Remote Agent Input Service"
const serviceDesc = "SYSTEM service that injects remote keyboard/mouse into the lock screen (Winlogon)."

type remotoService struct{}

func (m *remotoService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	changes <- svc.Status{State: svc.StartPending}
	LogInfo("input service starting")
	enableInjectPrivileges()

	ln, err := listenInputPipe()
	if err != nil {
		LogErrorErr("listen input pipe", err)
		return false, 1
	}
	defer ln.Close()

	stopHelper := make(chan struct{})
	go keepLockInputHelper(stopHelper)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-stopHelper:
					return
				default:
					LogWarn("pipe accept: %v", err)
					continue
				}
			}
			go serveInputPipeClient(conn)
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	LogInfo("input service running (pipe %s)", inputPipeName)

loop:
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			LogInfo("input service stop requested")
			break loop
		default:
			LogWarn("service unexpected control: %v", c.Cmd)
		}
	}

	close(stopHelper)
	stopLockInputHelper()
	changes <- svc.Status{State: svc.StopPending}
	return false, 0
}

func runAsService() error {
	err := svc.Run(serviceName, &remotoService{})
	if err == nil {
		return nil
	}
	isService, checkErr := svc.IsWindowsService()
	if checkErr == nil && isService {
		return err
	}
	LogInfo("running input service in console debug mode (%v)", err)
	return debug.Run(serviceName, &remotoService{})
}

func serviceStatusText() string {
	ok, running := queryServiceState()
	if !ok {
		return "未安装"
	}
	if running {
		if isInputServiceReachable() {
			return "运行中"
		}
		return "已安装(管道未就绪)"
	}
	return "已安装(已停止)"
}

func queryServiceState() (installed bool, running bool) {
	ok, run, err := queryServiceStateErr()
	if err != nil {
		return false, false
	}
	return ok, run
}

func queryServiceStateErr() (bool, bool, error) {
	return queryWindowsService(serviceName)
}

func fmtServiceHint() string {
	return fmt.Sprintf("锁屏输入需要 SYSTEM 服务「%s」。请在 Agent 界面点击安装（需管理员权限）。", serviceDisplay)
}
