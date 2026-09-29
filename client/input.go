package main

import "runtime"

func handleInput(msg map[string]interface{}) {
	if runtime.GOOS != "windows" {
		return
	}
	action, _ := msg["action"].(string)
	if action != "mousemove" {
		LogInfo("handleInput action=%v type=%v", action, msg["type"])
	}
	handleInputWindows(msg)
}
