//go:build !windows

package main

func runAsService() error                          { return nil }
func serviceStatusText() string                    { return "不支持" }
func isInputServiceReachable() bool                { return false }
func InstallInputServiceElevated() error           { return nil }
func UninstallInputServiceElevated() error         { return nil }
func runInstallServiceCLI()                        {}
func runUninstallServiceCLI()                      {}
func sendInputToService(msg InputIPCMsg) error     { return nil }
func fmtServiceHint() string                       { return "" }
func queryServiceState() (bool, bool)              { return false, false }
func ensureInputServiceRunning()                   {}
