package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
)

const capturePipeName = `\\.\pipe\RemotoAgentLockCapture`

const (
	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000
	dibRGB     = 0
	biRGB      = 0
)

var (
	gdi32                      = syscall.NewLazyDLL("gdi32.dll")
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetDesktopWindow       = user32.NewProc("GetDesktopWindow")
	procPrintWindow            = user32.NewProc("PrintWindow")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procCreateDCW              = gdi32.NewProc("CreateDCW")
	procEnumDesktopWindows     = user32.NewProc("EnumDesktopWindows")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
)

func startCapturePipe(captures chan chan []byte) {
	ln, err := winio.ListenPipe(capturePipeName, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)(A;;GA;;;SY)(A;;GA;;;BA)",
		InputBufferSize:    65536,
		OutputBufferSize:   1 << 20,
	})
	if err != nil {
		log.Printf("capture listen: %v", err)
		return
	}
	log.Printf("listening %s", capturePipeName)
	for {
		conn, err := ln.Accept()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go serveCapture(conn, captures)
	}
}

func serveCapture(conn net.Conn, captures chan chan []byte) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	resp := make(chan []byte, 1)
	select {
	case captures <- resp:
	case <-time.After(800 * time.Millisecond):
		return
	}
	var jpg []byte
	select {
	case jpg = <-resp:
	case <-time.After(1500 * time.Millisecond):
		return
	}
	if len(jpg) == 0 {
		return
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(jpg)))
	_, _ = conn.Write(hdr[:])
	_, _ = conn.Write(jpg)
}

var lastCaptureLog time.Time

func isSecureDesk(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "winlogon") || strings.Contains(n, "screensaver")
}

func captureLockJPEG() []byte {
	pinThreadDesktop()
	desk := desktopName(attachedDesk)
	if !isSecureDesk(desk) {
		// Unlocked Default: PrintWindow of top-level windows omits the
		// taskbar. The agent BitBlts the user desktop itself.
		return nil
	}
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	width, height := int(int32(w)), int(int32(h))
	if width <= 0 || height <= 0 {
		width, height = 1920, 1080
	}
	img, method, err := blitDesktop(width, height)
	if err != nil || img == nil {
		if time.Since(lastCaptureLog) > 2*time.Second {
			lastCaptureLog = time.Now()
			log.Printf("lock capture fail desk=%s method=%s err=%v", desktopName(attachedDesk), method, err)
		}
		return nil
	}
	if img.Bounds().Dx() > 1920 || img.Bounds().Dy() > 1080 {
		img = downscale(img, 2)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 55}); err != nil {
		log.Printf("lock capture jpeg: %v", err)
		return nil
	}
	if time.Since(lastCaptureLog) > 5*time.Second {
		lastCaptureLog = time.Now()
		log.Printf("lock capture ok method=%s desk=%s jpeg=%d black=%v", method, desktopName(attachedDesk), buf.Len(), mostlyBlack(img))
	}
	return buf.Bytes()
}

func blitDesktop(width, height int) (*image.RGBA, string, error) {
	if img, n, err := captureByWindows(width, height); err == nil && img != nil && !mostlyBlack(img) {
		return img, fmt.Sprintf("windows(%d)", n), nil
	}
	if hdc, _ := openDisplayDC(); hdc != 0 {
		img, err := bitBltDIB(hdc, width, height)
		_, _, _ = procDeleteDC.Call(hdc)
		if err == nil && img != nil && !mostlyBlack(img) {
			return img, "CreateDC", nil
		}
	}
	hdc, _, err := procGetDC.Call(0)
	if hdc == 0 {
		return nil, "GetDC", fmt.Errorf("GetDC(0)=0: %v", err)
	}
	defer procReleaseDC.Call(0, hdc)
	if img, err := bitBltDIB(hdc, width, height); err == nil && img != nil && !mostlyBlack(img) {
		return img, "BitBlt", nil
	}
	if img, err := printDesktopDIB(hdc, width, height); err == nil && img != nil && !mostlyBlack(img) {
		return img, "PrintDesktop", nil
	}
	if img, n, err := captureByWindows(width, height); err == nil && img != nil {
		return img, fmt.Sprintf("windows-black(%d)", n), nil
	}
	return nil, "all", fmt.Errorf("no frame from Winlogon capture")
}

func openDisplayDC() (uintptr, error) {
	name, _ := syscall.UTF16PtrFromString("DISPLAY")
	hdc, _, err := procCreateDCW.Call(uintptr(unsafe.Pointer(name)), 0, 0, 0)
	if hdc == 0 {
		return 0, err
	}
	return hdc, nil
}

type winRECT struct {
	Left, Top, Right, Bottom int32
}

type dibCanvas struct {
	hdc      uintptr
	memDC    uintptr
	hbm      uintptr
	old      uintptr
	bits     unsafe.Pointer
	width    int
	height   int
	fromGetDC bool
}

type bitmapInfo struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func openDIB(width, height int) (*dibCanvas, error) {
	fromGetDC := true
	hdc, _, err := procGetDC.Call(0)
	if hdc == 0 {
		fromGetDC = false
		hdc, err = openDisplayDC()
		if hdc == 0 {
			return nil, fmt.Errorf("no screen DC: %v", err)
		}
	}
	memDC, _, err := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		procReleaseDC.Call(0, hdc)
		return nil, fmt.Errorf("CreateCompatibleDC: %v", err)
	}
	var bi bitmapInfo
	bi.Size = 40
	bi.Width = int32(width)
	bi.Height = -int32(height)
	bi.Planes = 1
	bi.BitCount = 32
	bi.Compression = biRGB
	var bits unsafe.Pointer
	hbm, _, err := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&bi)), dibRGB, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || bits == nil {
		procDeleteDC.Call(memDC)
		procReleaseDC.Call(0, hdc)
		return nil, fmt.Errorf("CreateDIBSection: %v", err)
	}
	old, _, _ := procSelectObject.Call(memDC, hbm)
	return &dibCanvas{hdc: hdc, memDC: memDC, hbm: hbm, old: old, bits: bits, width: width, height: height, fromGetDC: fromGetDC}, nil
}

func (c *dibCanvas) close() {
	if c == nil {
		return
	}
	if c.old != 0 {
		_, _, _ = procSelectObject.Call(c.memDC, c.old)
	}
	if c.hbm != 0 {
		_, _, _ = procDeleteObject.Call(c.hbm)
	}
	if c.memDC != 0 {
		_, _, _ = procDeleteDC.Call(c.memDC)
	}
	if c.hdc != 0 {
		if c.fromGetDC {
			_, _, _ = procReleaseDC.Call(0, c.hdc)
		} else {
			_, _, _ = procDeleteDC.Call(c.hdc)
		}
	}
}

func (c *dibCanvas) image() *image.RGBA {
	_, _, _ = procSelectObject.Call(c.memDC, c.old)
	c.old = 0
	img := image.NewRGBA(image.Rect(0, 0, c.width, c.height))
	src := unsafe.Slice((*byte)(c.bits), c.width*c.height*4)
	for i := 0; i < c.width*c.height; i++ {
		si, di := i*4, i*4
		img.Pix[di+0] = src[si+2]
		img.Pix[di+1] = src[si+1]
		img.Pix[di+2] = src[si+0]
		img.Pix[di+3] = 255
	}
	return img
}

var (
	composeDC      uintptr
	composeOriginX int32
	composeOriginY int32
	composeCount   int
	enumPrintCB    = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		var r winRECT
		_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		w := int(r.Right - r.Left)
		h := int(r.Bottom - r.Top)
		if w <= 1 || h <= 1 {
			return 1
		}
		printWindowAt(composeDC, hwnd, int(r.Left-composeOriginX), int(r.Top-composeOriginY), w, h)
		composeCount++
		return 1
	})
)

func captureByWindows(width, height int) (*image.RGBA, int, error) {
	c, err := openDIB(width, height)
	if err != nil {
		return nil, 0, err
	}
	defer c.close()
	composeDC = c.memDC
	composeCount = 0
	vx, _, _ := procGetSystemMetrics.Call(76)
	vy, _, _ := procGetSystemMetrics.Call(77)
	composeOriginX, composeOriginY = int32(vx), int32(vy)
	if attachedDesk != 0 {
		_, _, _ = procEnumDesktopWindows.Call(attachedDesk, enumPrintCB, 0)
	}
	if composeCount == 0 {
		_, _, _ = procEnumWindows.Call(enumPrintCB, 0)
	}
	n := composeCount
	return c.image(), n, nil
}

func printWindowAt(destDC uintptr, hwnd uintptr, x, y, width, height int) {
	if destDC == 0 || hwnd == 0 || width <= 0 || height <= 0 {
		return
	}
	tmp, err := openDIB(width, height)
	if err != nil {
		return
	}
	defer tmp.close()
	ret, _, _ := procPrintWindow.Call(hwnd, tmp.memDC, 2)
	if ret == 0 {
		ret, _, _ = procPrintWindow.Call(hwnd, tmp.memDC, 0)
		if ret == 0 {
			return
		}
	}
	_, _, _ = procBitBlt.Call(destDC, toWin32(x), toWin32(y), uintptr(width), uintptr(height), tmp.memDC, 0, 0, srcCopy)
}

func toWin32(v int) uintptr {
	return uintptr(uint32(int32(v)))
}

func bitBltDIB(hdc uintptr, width, height int) (*image.RGBA, error) {
	c, err := openDIB(width, height)
	if err != nil {
		return nil, err
	}
	defer c.close()
	ret, _, err := procBitBlt.Call(c.memDC, 0, 0, uintptr(width), uintptr(height), hdc, 0, 0, srcCopy|captureBlt)
	if ret == 0 {
		ret, _, err = procBitBlt.Call(c.memDC, 0, 0, uintptr(width), uintptr(height), hdc, 0, 0, srcCopy)
		if ret == 0 {
			return nil, fmt.Errorf("BitBlt: %v", err)
		}
	}
	return c.image(), nil
}

func printDesktopDIB(hdc uintptr, width, height int) (*image.RGBA, error) {
	c, err := openDIB(width, height)
	if err != nil {
		return nil, err
	}
	defer c.close()
	desktop, _, _ := procGetDesktopWindow.Call()
	ret, _, err := procPrintWindow.Call(desktop, c.memDC, 2)
	if ret == 0 {
		ret, _, err = procPrintWindow.Call(desktop, c.memDC, 0)
		if ret == 0 {
			return nil, fmt.Errorf("PrintWindow: %v", err)
		}
	}
	return c.image(), nil
}

func mostlyBlack(img *image.RGBA) bool {
	if img == nil {
		return true
	}
	n := img.Bounds().Dx() * img.Bounds().Dy()
	if n == 0 {
		return true
	}
	step := 16
	if n < 1000 {
		step = 1
	}
	dark, total := 0, 0
	for i := 0; i < n; i += step {
		off := i * 4
		if int(img.Pix[off])+int(img.Pix[off+1])+int(img.Pix[off+2]) < 30 {
			dark++
		}
		total++
	}
	return total > 0 && dark*100/total > 96
}

func downscale(full *image.RGBA, scale int) *image.RGBA {
	if scale <= 1 {
		return full
	}
	width, height := full.Bounds().Dx(), full.Bounds().Dy()
	outW, outH := width/scale, height/scale
	if outW < 1 || outH < 1 {
		return full
	}
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for oy := 0; oy < outH; oy++ {
		sy := oy * scale
		for ox := 0; ox < outW; ox++ {
			sx := ox * scale
			copy(out.Pix[(oy*outW+ox)*4:(oy*outW+ox)*4+4], full.Pix[(sy*width+sx)*4:(sy*width+sx)*4+4])
		}
	}
	return out
}
