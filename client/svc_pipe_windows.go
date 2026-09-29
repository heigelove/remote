//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
)

var (
	svcConnMu   sync.Mutex
	svcConn     net.Conn
	helperConnMu sync.Mutex
	helperConn   net.Conn
)

func listenInputPipe() (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)(A;;GA;;;SY)(A;;GA;;;BA)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(inputPipeName, cfg)
}

func dialInputPipe(timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(inputPipeName, &timeout)
}

func sendInputToService(msg InputIPCMsg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	svcConnMu.Lock()
	defer svcConnMu.Unlock()
	if err := writeInputPipeLocked(b); err != nil {
		closeInputPipeLocked()
		if err2 := writeInputPipeLocked(b); err2 != nil {
			return fmt.Errorf("系统服务未运行或不可用: %w", err2)
		}
	}
	return nil
}

func writeInputPipeLocked(b []byte) error {
	if svcConn == nil {
		conn, err := dialInputPipe(800 * time.Millisecond)
		if err != nil {
			return err
		}
		svcConn = conn
	}
	_ = svcConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := svcConn.Write(b)
	return err
}

func closeInputPipeLocked() {
	if svcConn != nil {
		_ = svcConn.Close()
		svcConn = nil
	}
}

func isInputServiceReachable() bool {
	conn, err := dialInputPipe(400 * time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func listenNamedPipe(name string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)(A;;GA;;;SY)(A;;GA;;;BA)",
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(name, cfg)
}

func dialNamedPipe(name string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(name, &timeout)
}

func isLockHelperReachable() bool {
	conn, err := dialNamedPipe(lockInjectPipeName, 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func sendInputToLockHelper(msg InputIPCMsg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	helperConnMu.Lock()
	defer helperConnMu.Unlock()
	if err := writeLockHelperLocked(b); err != nil {
		closeLockHelperLocked()
		if err2 := writeLockHelperLocked(b); err2 != nil {
			return err2
		}
	}
	return nil
}

func writeLockHelperLocked(b []byte) error {
	if helperConn == nil {
		conn, err := dialNamedPipe(lockInjectPipeName, 800*time.Millisecond)
		if err != nil {
			return err
		}
		helperConn = conn
	}
	_ = helperConn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := helperConn.Write(b)
	return err
}

func closeLockHelperLocked() {
	if helperConn != nil {
		_ = helperConn.Close()
		helperConn = nil
	}
}

func serveInputPipeClient(conn net.Conn) {
	feedInputPipe(conn, func(msg InputIPCMsg) {
		if msg.Action == "keydown" || msg.Action == "keyup" {
			LogInfo("svc %s vk=0x%x helper=%v", msg.Action, msg.VK, isLockHelperReachable())
		}
		if err := sendInputToLockHelper(msg); err != nil {
			LogWarn("forward to lockhelper: %v", err)
			if msg.Action != "mousemove" {
				injectOnSecureDesktop(msg)
			}
		}
	})
}

func feedInputPipe(conn net.Conn, handle func(InputIPCMsg)) {
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
		var msg InputIPCMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			LogWarn("ipc bad message: %v", err)
			continue
		}
		handle(msg)
	}
}
