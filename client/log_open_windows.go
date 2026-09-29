//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func OpenLogFolder() error {
	dir := GetLogDir()
	if dir == "" {
		return fmt.Errorf("日志目录未初始化")
	}
	return exec.Command("explorer", dir).Start()
}
