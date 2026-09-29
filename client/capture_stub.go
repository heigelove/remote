//go:build !windows

package main

import (
	"fmt"
	"image"
)

func captureScreen() (*image.RGBA, error) {
	return nil, fmt.Errorf("screen capture only supported on windows")
}
