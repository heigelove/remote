//go:build !windows

package main

func currentCursorShape() string { return "arrow" }

func requestCursorSample() {}
