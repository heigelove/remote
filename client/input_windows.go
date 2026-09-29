//go:build windows

package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procMouseEvent       = user32.NewProc("mouse_event")
	procKeybdEvent       = user32.NewProc("keybd_event")
	procSendInput        = user32.NewProc("SendInput")
	procLockWorkStation  = user32.NewProc("LockWorkStation")
	procOpenInputDesktop = user32.NewProc("OpenInputDesktop")
	procSetThreadDesktop = user32.NewProc("SetThreadDesktop")
	procCloseDesktop     = user32.NewProc("CloseDesktop")
	procGetThreadDesktop = user32.NewProc("GetThreadDesktop")
	procGetCurrentThread = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThread")
	procMapVirtualKey    = user32.NewProc("MapVirtualKeyW")
)

const (
	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800

	keyEventKeyUp    = 0x0002
	keyEventUnicode  = 0x0004
	keyEventExtended = 0x0001

	inputMouse    = 0
	inputKeyboard = 1
	desktopAll    = 0x01FF
)

// winMouseINPUT mirrors INPUT{type=MOUSE} on amd64 (40 bytes).
type winMouseINPUT struct {
	Type      uint32
	Padding   uint32
	Dx        int32
	Dy        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Padding2  uint32
	ExtraInfo uintptr
}

type winKeyINPUT struct {
	Type    uint32
	Padding uint32
	Ki      winKEYBDINPUT
}

type winKEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
	Pad       [8]byte
}

var (
	inputCh     chan InputIPCMsg
	inputOnce   sync.Once
	svcReadyAt  atomic.Int64
	svcReady    atomic.Bool
	lastDeskOK  atomic.Bool
	lockAt      atomic.Int64
	lockOn      atomic.Bool
	modCtrl     bool
	modAlt      bool
	modShift    bool
	modWin      bool
)

func startInputWorker() {
	inputOnce.Do(func() {
		inputCh = make(chan InputIPCMsg, 512)
		go inputWorker()
	})
}

func inputWorker() {
	// Pin this goroutine to one OS thread with NO windows so
	// SetThreadDesktop / SendInput actually reach the interactive desktop.
	runtime.LockOSThread()
	attachInputDesktop()
	LogInfo("input worker thread started, desktop attached=%v", lastDeskOK.Load())
	for msg := range inputCh {
		if msg.Action != "mousemove" {
			attachInputDesktop()
		}
		applyInputMsg(msg)
	}
}

var inputDesk uintptr

func attachInputDesktop() {
	hDesk, _, _ := procOpenInputDesktop.Call(0, 0, desktopAll)
	if hDesk == 0 {
		lastDeskOK.Store(false)
		return
	}
	name := desktopName(hDesk)
	if isSecureDesktopName(name) {
		_, _, _ = procCloseDesktop.Call(hDesk)
		lastDeskOK.Store(false)
		return
	}
	if inputDesk != 0 && desktopName(inputDesk) == name {
		_, _, _ = procCloseDesktop.Call(hDesk)
		lastDeskOK.Store(true)
		return
	}
	r, _, err := procSetThreadDesktop.Call(hDesk)
	if r == 0 {
		lastDeskOK.Store(false)
		LogWarn("SetThreadDesktop failed: %v", err)
		_, _, _ = procCloseDesktop.Call(hDesk)
		return
	}
	if inputDesk != 0 && inputDesk != hDesk {
		_, _, _ = procCloseDesktop.Call(inputDesk)
	}
	inputDesk = hDesk
	lastDeskOK.Store(true)
}

func handleInputWindows(msg map[string]interface{}) {
	startInputWorker()

	action, _ := msg["action"].(string)
	x, _ := toInt(msg["x"])
	y, _ := toInt(msg["y"])
	sx, sy := mapImageToScreen(x, y)
	btn, _ := msg["button"].(string)
	delta, _ := toInt(msg["delta"])
	vk, _ := toInt(msg["vk"])
	ch, _ := msg["char"].(string)
	text, _ := msg["text"].(string)

	ipc := InputIPCMsg{
		Action:   action,
		X:        sx,
		Y:        sy,
		Button:   btn,
		Delta:    delta,
		VK:       vk,
		Char:     ch,
		Text:     text,
		Ctrl:     toBool(msg["ctrl"]),
		Alt:      toBool(msg["alt"]),
		Shift:    toBool(msg["shift"]),
		Meta:     toBool(msg["meta"]),
		Extended: toBool(msg["extended"]),
	}

	if action != "mousemove" {
		LogInfo("input %s button=%s img=(%d,%d) screen=(%d,%d) desk=%v locked=%v svc=%v",
			action, btn, x, y, sx, sy, lastDeskOK.Load(), cachedLocked(), cachedServiceReady())
	}

	if cachedLocked() {
		ensureInputServiceRunning()
		if err := sendInputToService(ipc); err != nil {
			LogWarn("lock-screen ipc: %v", err)
		} else {
			return
		}
	}

	select {
	case inputCh <- ipc:
	default:
		if action != "mousemove" {
			LogWarn("input queue full, dropping %s", action)
		}
	}
}

func cachedLocked() bool {
	now := time.Now().UnixMilli()
	if now-lockAt.Load() < 400 {
		return lockOn.Load()
	}
	// Only the input desktop name is reliable. LogonUI.exe often stays
	// running after unlock; treating that as locked keeps capture/input
	// on the helper (PrintWindow, no taskbar, clicks go nowhere).
	v := isWorkstationLocked()
	lockOn.Store(v)
	lockAt.Store(now)
	return v
}

func cachedServiceReady() bool {
	now := time.Now().UnixMilli()
	if now-svcReadyAt.Load() < 5000 {
		return svcReady.Load()
	}
	ok := isInputServiceReachable()
	svcReady.Store(ok)
	svcReadyAt.Store(now)
	return ok
}

func applyInputMsg(msg InputIPCMsg) {
	switch msg.Action {
	case "mousemove":
		moveCursor(msg.X, msg.Y)
	case "mousedown":
		mouseButtonAt(msg.X, msg.Y, msg.Button, true)
	case "mouseup":
		mouseButtonAt(msg.X, msg.Y, msg.Button, false)
	case "click":
		mouseButtonAt(msg.X, msg.Y, msg.Button, true)
		time.Sleep(20 * time.Millisecond)
		mouseButtonAt(msg.X, msg.Y, msg.Button, false)
	case "dblclick":
		// Real double-click is already two mousedown/up pairs from the client.
		// Do not inject a third click here.
	case "wheel":
		moveCursor(msg.X, msg.Y)
		mouseWheel(msg.Delta)
	case "keydown":
		injectKey(msg, false)
	case "keyup":
		injectKey(msg, true)
	case "type":
		for _, r := range msg.Text {
			sendUnicodeRune(r, false)
			sendUnicodeRune(r, true)
		}
	}
	switch msg.Action {
	case "mousemove", "mousedown", "mouseup", "click", "wheel":
		requestCursorSample()
	}
}

func buttonFlag(down bool, button string) uint32 {
	switch button {
	case "right":
		if down {
			return mouseEventRightDown
		}
		return mouseEventRightUp
	case "middle":
		if down {
			return mouseEventMiddleDown
		}
		return mouseEventMiddleUp
	default:
		if down {
			return mouseEventLeftDown
		}
		return mouseEventLeftUp
	}
}

func moveCursor(screenX, screenY int) {
	_, _, _ = procSetCursorPos.Call(uintptr(uint32(screenX)), uintptr(uint32(screenY)))
}

func mouseButtonAt(screenX, screenY int, button string, down bool) {
	moveCursor(screenX, screenY)

	flag := buttonFlag(down, button)
	in := winMouseINPUT{Type: inputMouse, Flags: flag}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret == 0 {
		_, _, _ = procMouseEvent.Call(uintptr(flag), 0, 0, 0, 0)
		action := "mousedown"
		if !down {
			action = "mouseup"
		}
		if svcErr := sendInputToService(InputIPCMsg{Action: action, X: screenX, Y: screenY, Button: button}); svcErr != nil {
			LogWarn("SendInput button failed flags=0x%x err=%v size=%d svc=%v", flag, err, unsafe.Sizeof(in), svcErr)
		} else {
			LogWarn("SendInput button failed flags=0x%x err=%v, forwarded to input service", flag, err)
		}
	}
}

func injectKey(msg InputIPCMsg, up bool) {
	vk := msg.VK
	if isModifierVK(vk) {
		sendVK(vk, up, msg.Extended || isExtendedVK(vk))
		setModFromVK(vk, !up)
		return
	}
	if !up {
		syncMods(msg.Ctrl, msg.Alt, msg.Shift, msg.Meta)
	}
	if vk != 0 {
		sendVK(vk, up, msg.Extended || isExtendedVK(vk))
		return
	}
	if msg.Char != "" {
		for _, r := range msg.Char {
			sendUnicodeRune(r, up)
		}
	}
}

func isModifierVK(vk int) bool {
	switch vk {
	case 0x10, 0x11, 0x12, 0x5B, 0x5C, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

func isExtendedVK(vk int) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E,
		0x5B, 0x5C, 0x5D, 0x6F, 0xA3, 0xA5:
		return true
	}
	return false
}

func setModFromVK(vk int, down bool) {
	switch vk {
	case 0x11, 0xA2, 0xA3:
		modCtrl = down
	case 0x12, 0xA4, 0xA5:
		modAlt = down
	case 0x10, 0xA0, 0xA1:
		modShift = down
	case 0x5B, 0x5C:
		modWin = down
	}
}

func syncMods(ctrl, alt, shift, win bool) {
	setMod(ctrl, &modCtrl, 0x11, false)
	setMod(alt, &modAlt, 0x12, false)
	setMod(shift, &modShift, 0x10, false)
	setMod(win, &modWin, 0x5B, true)
}

func setMod(want bool, have *bool, vk int, extended bool) {
	if want == *have {
		return
	}
	sendVK(vk, !want, extended)
	*have = want
}

func sendVK(vk int, up, extended bool) {
	if vk == 0 {
		return
	}
	scan, _, _ := procMapVirtualKey.Call(uintptr(vk), 0)
	flags := uint32(0)
	if up {
		flags |= keyEventKeyUp
	}
	if extended {
		flags |= keyEventExtended
	}
	in := winKeyINPUT{
		Type: inputKeyboard,
		Ki: winKEYBDINPUT{
			Vk:    uint16(vk),
			Scan:  uint16(scan),
			Flags: flags,
		},
	}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret == 0 {
		var keFlags uintptr
		if up {
			keFlags = keyEventKeyUp
		}
		_, _, _ = procKeybdEvent.Call(uintptr(vk), scan, keFlags, 0)
		action := "keydown"
		if up {
			action = "keyup"
		}
		if svcErr := sendInputToService(InputIPCMsg{Action: action, VK: vk, Extended: extended}); svcErr != nil {
			LogWarn("SendInput key failed vk=0x%x err=%v svc=%v", vk, err, svcErr)
		}
	}
}

func mouseWheel(delta int) {
	in := winMouseINPUT{Type: inputMouse, MouseData: uint32(delta), Flags: mouseEventWheel}
	ret, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret == 0 {
		_, _, _ = procMouseEvent.Call(mouseEventWheel, 0, 0, uintptr(uint32(delta)), 0)
	}
}

func sendUnicodeRune(r rune, up bool) {
	flags := uint32(keyEventUnicode)
	if up {
		flags |= keyEventKeyUp
	}
	in := winKeyINPUT{
		Type: inputKeyboard,
		Ki: winKEYBDINPUT{
			Scan:  uint16(r),
			Flags: flags,
		},
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}

func toBool(v interface{}) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case string:
		return n == "true" || n == "1"
	default:
		return false
	}
}
