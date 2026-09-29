// Lock-screen input helper: runs in the console session on Winlogon as SYSTEM.
// Started by Remote Agent Input Service via CreateProcessAsUser(winlogon token).
// No GUI — a Wails window on Winlogon would steal focus from LogonUI's password box.
package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
)

const (
	pipeName = `\\.\pipe\RemotoAgentLockInject`

	winstaAllAccess  = 0x037F
	desktopAllAccess = 0x01FF

	inputMouse    = 0
	inputKeyboard = 1

	mouseEventMove      = 0x0001
	mouseEventLeftDown  = 0x0002
	mouseEventLeftUp    = 0x0004
	mouseEventRightDown = 0x0008
	mouseEventRightUp   = 0x0010
	mouseEventWheel     = 0x0800
	mouseEventAbsolute  = 0x8000
	mouseEventVirtual   = 0x4000

	keyEventExtended = 0x0001
	keyEventKeyUp    = 0x0002
	keyEventUnicode  = 0x0004
	keyEventScanCode = 0x0008

	smCXScreen = 0
	smCYScreen = 1
)

type ipcMsg struct {
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
}

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
	user32                   = syscall.NewLazyDLL("user32.dll")
	procSendInput            = user32.NewProc("SendInput")
	procSetCursorPos         = user32.NewProc("SetCursorPos")
	procMapVirtualKey        = user32.NewProc("MapVirtualKeyW")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procOpenWindowStationW   = user32.NewProc("OpenWindowStationW")
	procSetProcessWindowStation = user32.NewProc("SetProcessWindowStation")
	procOpenDesktopW         = user32.NewProc("OpenDesktopW")
	procOpenInputDesktop     = user32.NewProc("OpenInputDesktop")
	procSetThreadDesktop     = user32.NewProc("SetThreadDesktop")
	procGetThreadDesktop     = user32.NewProc("GetThreadDesktop")
	procCloseDesktop         = user32.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32.NewProc("GetUserObjectInformationW")
	procEnumThreadWindows    = user32.NewProc("EnumThreadWindows")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procKeybdEvent           = user32.NewProc("keybd_event")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThreadId   = kernel32.NewProc("GetCurrentThreadId")
	procGetCurrentThread     = kernel32.NewProc("GetCurrentThread")
)

var (
	attachedDesk uintptr
	wokeLockUI   bool
	modCtrl, modAlt, modShift, modWin bool
)

func main() {
	runtime.LockOSThread()
	if exe, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(exe))
		setupFileLog(filepath.Dir(exe))
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("lockhelper start pid=%d", os.Getpid())

	destroyThreadWindows()
	if err := attachWinlogon(); err != nil {
		log.Printf("attach winlogon: %v (will retry on each event)", err)
	}

	ln, err := listenPipe()
	if err != nil {
		log.Fatalf("listen %s: %v", pipeName, err)
	}
	defer ln.Close()
	log.Printf("listening %s desktop=%s", pipeName, currentDesktopName())

	jobs := make(chan ipcMsg, 1024)
	captures := make(chan chan []byte, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Printf("accept: %v", err)
				time.Sleep(200 * time.Millisecond)
				continue
			}
			go readConn(conn, jobs)
		}
	}()
	go startCapturePipe(captures)

	for {
		select {
		case msg := <-jobs:
			if msg.Action == "quit" {
				log.Printf("quit")
				return
			}
			_ = attachWinlogon()
			pinThreadDesktop()
			if !isSecureDesk(desktopName(attachedDesk)) {
				continue
			}
			if msg.Action == "keydown" || msg.Action == "keyup" {
				log.Printf("inject %s vk=0x%x shift=%v desk=%s thread=%s", msg.Action, msg.VK, msg.Shift, desktopName(attachedDesk), currentDesktopName())
			}
			if !wokeLockUI && msg.Action != "mousemove" {
				wakeLockScreen()
				wokeLockUI = true
			}
			apply(msg)
		case resp := <-captures:
			_ = attachWinlogon()
			pinThreadDesktop()
			resp <- captureLockJPEG()
		}
	}
}

func setupFileLog(dir string) {
	logDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, "lockhelper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.SetOutput(f)
}

func listenPipe() (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)(A;;GA;;;SY)(A;;GA;;;BA)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(pipeName, cfg)
}

func readConn(conn net.Conn, jobs chan<- ipcMsg) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg ipcMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Printf("bad ipc: %v", err)
			continue
		}
		if msg.Action == "mousemove" {
			select {
			case jobs <- msg:
			default:
			}
			continue
		}
		jobs <- msg
	}
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func attachWinlogon() error {
	hWinsta, _, err := procOpenWindowStationW.Call(uintptr(unsafe.Pointer(utf16Ptr("WinSta0"))), 0, winstaAllAccess)
	if hWinsta == 0 {
		return err
	}
	if r, _, err := procSetProcessWindowStation.Call(hWinsta); r == 0 {
		return err
	}
	hDesk, _, err := procOpenInputDesktop.Call(0, 0, desktopAllAccess)
	if hDesk == 0 {
		hDesk, _, err = procOpenDesktopW.Call(uintptr(unsafe.Pointer(utf16Ptr("Winlogon"))), 0, 0, desktopAllAccess)
		if hDesk == 0 {
			return err
		}
	}
	name := desktopName(hDesk)
	if attachedDesk != 0 && desktopName(attachedDesk) == name && name != "" {
		_, _, _ = procCloseDesktop.Call(hDesk)
		pinThreadDesktop()
		return nil
	}
	if r, _, err := procSetThreadDesktop.Call(hDesk); r == 0 {
		_, _, _ = procCloseDesktop.Call(hDesk)
		return err
	}
	if attachedDesk != 0 && attachedDesk != hDesk {
		old := attachedDesk
		attachedDesk = hDesk
		_, _, _ = procCloseDesktop.Call(old)
	} else {
		attachedDesk = hDesk
	}
	log.Printf("attached desktop=%s", name)
	return nil
}

func pinThreadDesktop() {
	if attachedDesk == 0 {
		return
	}
	if r, _, err := procSetThreadDesktop.Call(attachedDesk); r == 0 {
		log.Printf("SetThreadDesktop pin failed: %v", err)
	}
}

func desktopName(hDesk uintptr) string {
	if hDesk == 0 {
		return ""
	}
	buf := make([]uint16, 256)
	var needed uint32
	ret, _, _ := procGetUserObjectInformationW.Call(hDesk, 2, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func currentDesktopName() string {
	th, _, _ := procGetCurrentThread.Call()
	h, _, _ := procGetThreadDesktop.Call(th)
	return desktopName(h)
}

var destroyWndCB = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
	_, _, _ = procDestroyWindow.Call(hwnd)
	return 1
})

func destroyThreadWindows() {
	tid, _, _ := procGetCurrentThreadId.Call()
	_, _, _ = procEnumThreadWindows.Call(tid, destroyWndCB, 0)
}

func wakeLockScreen() {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		w, h = 1920, 1080
	}
	x := int(w) / 2
	// Dismiss the clock/wallpaper first, then click the password field.
	clickAbs(x, int(h)*55/100)
	time.Sleep(250 * time.Millisecond)
	clickAbs(x, int(h)*62/100)
	time.Sleep(250 * time.Millisecond)
	log.Printf("woke lock UI click=(%d,%d) screen=%dx%d desk=%s", x, int(h)*62/100, w, h, desktopName(attachedDesk))
}

func apply(msg ipcMsg) {
	switch msg.Action {
	case "mousemove":
		moveAbs(msg.X, msg.Y)
	case "mousedown":
		moveAbs(msg.X, msg.Y)
		mouseBtn(msg.Button, true)
	case "mouseup":
		moveAbs(msg.X, msg.Y)
		mouseBtn(msg.Button, false)
	case "click":
		moveAbs(msg.X, msg.Y)
		mouseBtn(msg.Button, true)
		time.Sleep(20 * time.Millisecond)
		mouseBtn(msg.Button, false)
	case "wheel":
		moveAbs(msg.X, msg.Y)
		sendMouse(0, 0, uint32(msg.Delta), mouseEventWheel)
	case "keydown":
		injectKey(msg, false)
	case "keyup":
		injectKey(msg, true)
	case "type":
		for _, r := range msg.Text {
			sendUnicode(r, false)
			sendUnicode(r, true)
		}
	}
}

func injectKey(msg ipcMsg, up bool) {
	vk := msg.VK
	if isMod(vk) {
		sendVK(vk, up, msg.Extended || isExt(vk))
		setMod(vk, !up)
		return
	}
	if !up {
		syncMod(msg.Ctrl, &modCtrl, 0x11, false)
		syncMod(msg.Alt, &modAlt, 0x12, false)
		syncMod(msg.Shift, &modShift, 0x10, false)
		syncMod(msg.Meta, &modWin, 0x5B, true)
	}
	if vk != 0 {
		if r := vkToRune(vk, msg.Shift); r != 0 {
			sendUnicode(r, up)
			return
		}
		sendVK(vk, up, msg.Extended || isExt(vk))
		return
	}
	for _, r := range msg.Char {
		sendUnicode(r, up)
	}
}

func isMod(vk int) bool {
	switch vk {
	case 0x10, 0x11, 0x12, 0x5B, 0x5C, 0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5:
		return true
	}
	return false
}

func isExt(vk int) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2D, 0x2E, 0x5B, 0x5C, 0x5D, 0x6F, 0xA3, 0xA5:
		return true
	}
	return false
}

func setMod(vk int, down bool) {
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

func syncMod(want bool, have *bool, vk int, extended bool) {
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
	in := winKeyINPUT{Type: inputKeyboard, Ki: winKEYBDINPUT{Vk: uint16(vk), Scan: uint16(scan), Flags: flags}}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret != 0 {
		return
	}
	log.Printf("SendInput vk=0x%x scan=0x%x up=%v err=%v, try keybd_event", vk, scan, up, err)
	var keFlags uintptr
	if up {
		keFlags = keyEventKeyUp
	}
	if extended {
		keFlags |= keyEventExtended
	}
	_, _, _ = procKeybdEvent.Call(uintptr(vk), scan, keFlags, 0)
}

func vkToRune(vk int, shift bool) rune {
	switch {
	case vk >= 0x30 && vk <= 0x39 && !shift:
		return rune(vk)
	case vk >= 0x41 && vk <= 0x5A:
		if shift {
			return rune(vk)
		}
		return rune(vk + 32)
	case vk == 0x20:
		return ' '
	}
	return 0
}

func sendUnicode(r rune, up bool) {
	flags := uint32(keyEventUnicode)
	if up {
		flags |= keyEventKeyUp
	}
	in := winKeyINPUT{Type: inputKeyboard, Ki: winKEYBDINPUT{Scan: uint16(r), Flags: flags}}
	_, _, _ = procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func screenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 {
		w = 1
	}
	if h == 0 {
		h = 1
	}
	return int(w), int(h)
}

func toAbs(x, y int) (int32, int32) {
	w, h := screenSize()
	return int32(x * 65535 / w), int32(y * 65535 / h)
}

func moveAbs(x, y int) {
	_, _, _ = procSetCursorPos.Call(uintptr(uint32(x)), uintptr(uint32(y)))
	dx, dy := toAbs(x, y)
	sendMouse(dx, dy, 0, mouseEventMove|mouseEventAbsolute|mouseEventVirtual)
}

func clickAbs(x, y int) {
	moveAbs(x, y)
	mouseBtn("left", true)
	time.Sleep(15 * time.Millisecond)
	mouseBtn("left", false)
}

func mouseBtn(button string, down bool) {
	var flag uint32
	switch button {
	case "right":
		if down {
			flag = mouseEventRightDown
		} else {
			flag = mouseEventRightUp
		}
	default:
		if down {
			flag = mouseEventLeftDown
		} else {
			flag = mouseEventLeftUp
		}
	}
	sendMouse(0, 0, 0, flag)
}

func sendMouse(dx, dy int32, data uint32, flags uint32) {
	in := winMouseINPUT{Type: inputMouse, Dx: dx, Dy: dy, MouseData: data, Flags: flags}
	ret, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if ret == 0 {
		log.Printf("SendInput mouse flags=0x%x err=%v", flags, err)
	}
}
