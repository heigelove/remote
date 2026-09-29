//go:build !windows

package main

func IsAutoStartEnabled() bool { return false }
func SetAutoStart(enable bool) error {
	return nil
}
func isWorkstationLocked() bool { return false }
