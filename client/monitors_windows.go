//go:build windows

package main

import (
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const monitorInfoPrimary = 0x00000001

type winRECT struct {
	Left, Top, Right, Bottom int32
}

type monitorInfoEx struct {
	Size    uint32
	Monitor winRECT
	Work    winRECT
	Flags   uint32
	Device  [32]uint16
}

var (
	procEnumDisplayMonitors = user32Cap.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32Cap.NewProc("GetMonitorInfoW")
	enumMonCB               = syscall.NewCallback(enumMonitor)
	enumMu                  sync.Mutex
	enumBuf                 []DisplayInfo
	displaysCache           []DisplayInfo
	displaysCacheAt         time.Time
)

func enumMonitor(hMonitor, hdc, lprcMonitor, dwData uintptr) uintptr {
	var mi monitorInfoEx
	mi.Size = uint32(unsafe.Sizeof(mi))
	r, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&mi)))
	if r == 0 {
		return 1
	}
	enumBuf = append(enumBuf, DisplayInfo{
		X:       int(mi.Monitor.Left),
		Y:       int(mi.Monitor.Top),
		W:       int(mi.Monitor.Right - mi.Monitor.Left),
		H:       int(mi.Monitor.Bottom - mi.Monitor.Top),
		Primary: mi.Flags&monitorInfoPrimary != 0,
		Name:    syscall.UTF16ToString(mi.Device[:]),
	})
	return 1
}

func listDisplays() []DisplayInfo {
	enumMu.Lock()
	defer enumMu.Unlock()
	if len(displaysCache) > 0 && time.Since(displaysCacheAt) < time.Second {
		return append([]DisplayInfo(nil), displaysCache...)
	}
	enumBuf = enumBuf[:0]
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, enumMonCB, 0)
	out := append([]DisplayInfo(nil), enumBuf...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Y < out[j].Y
	})
	for i := range out {
		out[i].Index = i
	}
	displaysCache = out
	displaysCacheAt = time.Now()
	return out
}

func captureBounds() (x, y, width, height int) {
	idx := selectedDisplayIndex()
	if idx >= 0 {
		list := listDisplays()
		if idx < len(list) {
			d := list[idx]
			if d.W > 0 && d.H > 0 {
				return d.X, d.Y, d.W, d.H
			}
		}
	}
	return screenBounds()
}
