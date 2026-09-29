//go:build !windows

package main

import "fmt"

func OpenLogFolder() error {
	return fmt.Errorf("not supported")
}
