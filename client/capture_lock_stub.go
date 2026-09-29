//go:build !windows

package main

import "fmt"

func lockScreenCaptureNeeded() bool { return false }

func captureSecureDesktopJPEG() ([]byte, error) {
	return nil, fmt.Errorf("not locked")
}

func pullLockCaptureJPEG() ([]byte, error) {
	return nil, fmt.Errorf("not locked")
}
