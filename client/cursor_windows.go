//go:build windows

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	cursorShowing = 0x00000001
	idcArrow      = 32512
	idcIBeam      = 32513
	idcWait       = 32514
	idcCross      = 32515
	idcSizeNWSE   = 32642
	idcSizeNESW   = 32643
	idcSizeWE     = 32644
	idcSizeNS     = 32645
	idcSizeAll    = 32646
	idcNo         = 32648
	idcHand       = 32649
	idcAppStarting = 32650
	idcHelp       = 32651
)

type winCURSORINFO struct {
	cbSize  uint32
	flags   uint32
	hCursor uintptr
	x       int32
	y       int32
}

var (
	procGetCursorInfo = user32.NewProc("GetCursorInfo")
	procLoadCursorW   = user32.NewProc("LoadCursorW")
	sysCursorsOnce    sync.Once
	sysCursors        map[uintptr]string
	cursorKick        atomic.Bool
)

func loadSysCursors() {
	sysCursorsOnce.Do(func() {
		sysCursors = map[uintptr]string{}
		add := func(id uintptr, name string) {
			h, _, _ := procLoadCursorW.Call(0, id)
			if h != 0 {
				sysCursors[h] = name
			}
		}
		add(idcIBeam, "text")
		add(idcArrow, "arrow")
		add(idcHand, "pointer")
		add(idcWait, "wait")
		add(idcAppStarting, "progress")
		add(idcSizeWE, "ew-resize")
		add(idcSizeNS, "ns-resize")
		add(idcSizeNWSE, "nwse-resize")
		add(idcSizeNESW, "nesw-resize")
		add(idcSizeAll, "move")
		add(idcNo, "not-allowed")
		add(idcCross, "crosshair")
		add(idcHelp, "help")
	})
}

func currentCursorShape() string {
	loadSysCursors()
	var info winCURSORINFO
	info.cbSize = uint32(unsafe.Sizeof(info))
	r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.hCursor == 0 {
		return "arrow"
	}
	if name, ok := sysCursors[info.hCursor]; ok {
		return name
	}
	return "arrow"
}

func requestCursorSample() {
	if !cursorKick.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		cursorKick.Store(false)
		runtimeAgent.pushCursorShape()
	}()
}
