//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/Microsoft/go-winio"
)

func lockScreenCaptureNeeded() bool {
	return cachedLocked()
}

func captureSecureDesktopJPEG() ([]byte, error) {
	if !cachedLocked() {
		return nil, fmt.Errorf("not locked")
	}
	return pullLockCaptureJPEG()
}

func pullLockCaptureJPEG() ([]byte, error) {
	timeout := 800 * time.Millisecond
	conn, err := winio.DialPipe(lockCapturePipeName, &timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n < 100 || n > 8<<20 {
		return nil, fmt.Errorf("bad lock jpeg size %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	x, y, w, h := screenBounds()
	sc := scaleFor(w, h)
	iw, ih := w, h
	if sc > 1 {
		iw, ih = w/sc, h/sc
	}
	setCaptureMapping(x, y, w, h, iw, ih)
	return buf, nil
}
