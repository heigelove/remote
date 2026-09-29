//go:build !windows

package main

func listDisplays() []DisplayInfo { return nil }

func captureBounds() (x, y, width, height int) { return 0, 0, 0, 0 }
