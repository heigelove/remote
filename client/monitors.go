package main

import "sync/atomic"

type DisplayInfo struct {
	Index   int    `json:"index"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Primary bool   `json:"primary"`
	Name    string `json:"name"`
}

// selectedDisplay: -1 = entire virtual desktop, 0+ = listDisplays() index.
var selectedDisplay atomic.Int32

func setSelectedDisplay(idx int) {
	if idx < -1 {
		idx = -1
	}
	selectedDisplay.Store(int32(idx))
	LogInfo("capture display index=%d", idx)
}

func selectedDisplayIndex() int {
	return int(selectedDisplay.Load())
}

func init() {
	selectedDisplay.Store(-1)
}

func jsonInt(v interface{}) (int, bool) {
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
