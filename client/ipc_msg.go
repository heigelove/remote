package main

import "time"

const inputPipeName = `\\.\pipe\RemotoAgentInput`
const lockInjectPipeName = `\\.\pipe\RemotoAgentLockInject`
const lockCapturePipeName = `\\.\pipe\RemotoAgentLockCapture`

// InputIPCMsg is sent from user-session agent to SYSTEM service.
type InputIPCMsg struct {
	Action   string `json:"action"`
	X        int    `json:"x,omitempty"`
	Y        int    `json:"y,omitempty"`
	Button   string `json:"button,omitempty"`
	Delta    int    `json:"delta,omitempty"`
	VK       int    `json:"vk,omitempty"`
	Char     string `json:"char,omitempty"`
	Text     string `json:"text,omitempty"`
	Ctrl     bool   `json:"ctrl,omitempty"`
	Alt      bool   `json:"alt,omitempty"`
	Shift    bool   `json:"shift,omitempty"`
	Meta     bool   `json:"meta,omitempty"`
	Extended bool   `json:"extended,omitempty"`
	TS       int64  `json:"ts,omitempty"`
}

func newInputIPC(action string) InputIPCMsg {
	return InputIPCMsg{Action: action, TS: time.Now().UnixMilli()}
}
