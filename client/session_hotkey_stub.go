//go:build !windows

package main

import "context"

func startSessionHotkeyHook(ctx context.Context) {}

func stopSessionHotkeyHook() {}
