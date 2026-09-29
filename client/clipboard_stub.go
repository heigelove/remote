//go:build !windows

package main

func clipboardSequence() uint32 { return 0 }

func getClipboardText() (string, error) { return "", nil }

func setClipboardText(s string) error { return nil }
