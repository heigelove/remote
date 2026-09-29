//go:build windows

package main

func lockWorkstation() {
	r, _, err := procLockWorkStation.Call()
	if r == 0 {
		LogWarn("LockWorkStation failed: %v", err)
		return
	}
	LogInfo("workstation lock requested")
}
