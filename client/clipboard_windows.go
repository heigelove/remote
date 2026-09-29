//go:build windows

package main

import (
	"fmt"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	clipUser32   = syscall.NewLazyDLL("user32.dll")
	clipKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard              = clipUser32.NewProc("OpenClipboard")
	procCloseClipboard             = clipUser32.NewProc("CloseClipboard")
	procEmptyClipboard             = clipUser32.NewProc("EmptyClipboard")
	procGetClipboardData           = clipUser32.NewProc("GetClipboardData")
	procSetClipboardData           = clipUser32.NewProc("SetClipboardData")
	procGetClipboardSequenceNumber = clipUser32.NewProc("GetClipboardSequenceNumber")
	procIsClipboardFormatAvailable = clipUser32.NewProc("IsClipboardFormatAvailable")
	procGlobalAlloc                = clipKernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = clipKernel32.NewProc("GlobalLock")
	procGlobalUnlock               = clipKernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = clipKernel32.NewProc("GlobalFree")
)

func clipboardSequence() uint32 {
	n, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(n)
}

func openClipboardRetry() bool {
	for i := 0; i < 15; i++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			return true
		}
		time.Sleep(8 * time.Millisecond)
	}
	return false
}

func getClipboardText() (string, error) {
	ok, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText)
	if ok == 0 {
		return "", nil
	}
	if !openClipboardRetry() {
		return "", fmt.Errorf("OpenClipboard")
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", fmt.Errorf("GlobalLock")
	}
	defer procGlobalUnlock.Call(h)
	return utf16PtrToString(ptr), nil
}

func setClipboardText(s string) error {
	u := utf16.Encode([]rune(s))
	u = append(u, 0)
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	ptr, _, err := procGlobalLock.Call(h)
	if ptr == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock: %v", err)
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(u)), u)
	_, _, _ = procGlobalUnlock.Call(h)

	if !openClipboardRetry() {
		_, _, _ = procGlobalFree.Call(h)
		return fmt.Errorf("OpenClipboard")
	}
	defer procCloseClipboard.Call()
	_, _, _ = procEmptyClipboard.Call()
	r, _, err := procSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	return nil
}

func utf16PtrToString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	max := clipMaxBytes/2 + 1
	n := 0
	for n < max && *(*uint16)(unsafe.Pointer(ptr + uintptr(n)*2)) != 0 {
		n++
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), n)))
}
