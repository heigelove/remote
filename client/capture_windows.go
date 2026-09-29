//go:build windows

package main

import (
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                      = windows.NewLazySystemDLL("gdi32.dll")
	user32Cap                  = windows.NewLazySystemDLL("user32.dll")
	procGetDC                  = user32Cap.NewProc("GetDC")
	procReleaseDC              = user32Cap.NewProc("ReleaseDC")
	procGetDesktopWindow       = user32Cap.NewProc("GetDesktopWindow")
	procGetSystemMetrics       = user32Cap.NewProc("GetSystemMetrics")
	procPrintWindow            = user32Cap.NewProc("PrintWindow")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procGdiFlush               = gdi32.NewProc("GdiFlush")
)

const (
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXScreen        = 0
	smCYScreen        = 1
	srcCopy           = 0x00CC0020
	captureBlt        = 0x40000000
	biRGB             = 0
	dibRGBColors      = 0
	pwRenderFull      = 0x00000002
)

type bitmapInfoHeader struct {
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

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type captureJob struct {
	resp chan captureResult
}

type captureResult struct {
	img *image.RGBA
	err error
}

var (
	captureOnce sync.Once
	captureJobs chan captureJob
)

func startCaptureWorker() {
	captureOnce.Do(func() {
		captureJobs = make(chan captureJob)
		go captureWorker()
	})
}

func captureWorker() {
	runtime.LockOSThread()
	attachCaptureDesktop()
	LogInfo("capture worker thread started")
	for job := range captureJobs {
		img, err := captureScreenOnThread()
		job.resp <- captureResult{img: img, err: err}
	}
}

func attachCaptureDesktop() {
	name, _ := syscall.UTF16PtrFromString("Default")
	hDesk, _, _ := procOpenDesktopW.Call(uintptr(unsafe.Pointer(name)), 0, 0, desktopAll)
	if hDesk == 0 {
		hDesk, _, _ = procOpenInputDesktop.Call(0, 0, desktopAll)
		if hDesk == 0 {
			LogWarn("capture: open desktop failed")
			return
		}
		if isSecureDesktopName(desktopName(hDesk)) {
			_, _, _ = procCloseDesktop.Call(hDesk)
			return
		}
	}
	if r, _, err := procSetThreadDesktop.Call(hDesk); r == 0 {
		LogWarn("capture SetThreadDesktop failed: %v", err)
		_, _, _ = procCloseDesktop.Call(hDesk)
	}
}

func captureScreen() (*image.RGBA, error) {
	startCaptureWorker()
	job := captureJob{resp: make(chan captureResult, 1)}
	select {
	case captureJobs <- job:
	case <-time.After(2 * time.Second):
		return nil, fmt.Errorf("capture queue timeout")
	}
	select {
	case res := <-job.resp:
		return res.img, res.err
	case <-time.After(2 * time.Second):
		return nil, fmt.Errorf("capture timeout")
	}
}

func screenBounds() (x, y, width, height int) {
	vx, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	vy, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	vw, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	vh, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if int32(vw) > 0 && int32(vh) > 0 {
		return int(int32(vx)), int(int32(vy)), int(int32(vw)), int(int32(vh))
	}
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return 0, 0, int(int32(w)), int(int32(h))
}

func captureScreenOnThread() (*image.RGBA, error) {
	x, y, width, height := captureBounds()
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid screen size %dx%d", width, height)
	}

	img, err := blitScreen(x, y, width, height)
	originX, originY := x, y
	if err != nil && selectedDisplayIndex() < 0 {
		w, _, _ := procGetSystemMetrics.Call(smCXScreen)
		h, _, _ := procGetSystemMetrics.Call(smCYScreen)
		pw, ph := int(int32(w)), int(int32(h))
		if pw > 0 && ph > 0 && (pw != width || ph != height || x != 0 || y != 0) {
			img, err = blitScreen(0, 0, pw, ph)
			if err == nil {
				originX, originY = 0, 0
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return applyStreamSize(img, originX, originY), nil
}

func scaleFor(width, height int) int {
	if width > 1920 || height > 1080 {
		return 2
	}
	return 1
}

func blitScreen(x, y, width, height int) (*image.RGBA, error) {
	hdc, _, err := procGetDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("GetDC failed: %v", err)
	}
	defer procReleaseDC.Call(0, hdc)

	img, compatErr := bitBltCompatible(hdc, x, y, width, height)
	if compatErr == nil {
		return img, nil
	}
	img, dibErr := bitBltDIB(hdc, x, y, width, height)
	if dibErr == nil {
		return img, nil
	}
	img, pwErr := printDesktop(hdc, width, height)
	if pwErr == nil {
		return img, nil
	}
	return nil, fmt.Errorf("BitBlt failed (%v); DIB %v; PrintWindow %v", compatErr, dibErr, pwErr)
}

func bitBltCompatible(hdc uintptr, x, y, width, height int) (*image.RGBA, error) {
	memDC, _, err := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %v", err)
	}
	defer procDeleteDC.Call(memDC)

	hbm, _, err := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if hbm == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap: %v", err)
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(memDC, hbm)
	defer procSelectObject.Call(memDC, old)

	if !doBitBlt(memDC, hdc, x, y, width, height) {
		return nil, fmt.Errorf("BitBlt compatible")
	}
	_, _, _ = procGdiFlush.Call()
	return dibToRGBA(hdc, hbm, width, height)
}

func bitBltDIB(hdc uintptr, x, y, width, height int) (*image.RGBA, error) {
	memDC, _, err := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %v", err)
	}
	defer procDeleteDC.Call(memDC)

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(width)
	bi.Header.Height = -int32(height)
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	var bits unsafe.Pointer
	hbm, _, err := procCreateDIBSection.Call(
		memDC,
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if hbm == 0 || bits == nil {
		return nil, fmt.Errorf("CreateDIBSection: %v", err)
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(memDC, hbm)
	defer procSelectObject.Call(memDC, old)

	if !doBitBlt(memDC, hdc, x, y, width, height) {
		return nil, fmt.Errorf("BitBlt DIB")
	}
	_, _, _ = procGdiFlush.Call()

	src := unsafe.Slice((*byte)(bits), width*height*4)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < width*height; i++ {
		si := i * 4
		di := i * 4
		img.Pix[di+0] = src[si+2]
		img.Pix[di+1] = src[si+1]
		img.Pix[di+2] = src[si+0]
		img.Pix[di+3] = 255
	}
	return img, nil
}

func doBitBlt(memDC, hdc uintptr, x, y, width, height int) bool {
	ret, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(width), uintptr(height), hdc, toWin32Int(x), toWin32Int(y), srcCopy|captureBlt)
	if ret != 0 {
		return true
	}
	ret, _, _ = procBitBlt.Call(memDC, 0, 0, uintptr(width), uintptr(height), hdc, toWin32Int(x), toWin32Int(y), srcCopy)
	return ret != 0
}

func printDesktop(hdc uintptr, width, height int) (*image.RGBA, error) {
	desktop, _, _ := procGetDesktopWindow.Call()
	memDC, _, err := procCreateCompatibleDC.Call(hdc)
	if memDC == 0 {
		return nil, fmt.Errorf("PrintWindow DC: %v", err)
	}
	defer procDeleteDC.Call(memDC)
	hbm, _, err := procCreateCompatibleBitmap.Call(hdc, uintptr(width), uintptr(height))
	if hbm == 0 {
		return nil, fmt.Errorf("PrintWindow bitmap: %v", err)
	}
	defer procDeleteObject.Call(hbm)
	old, _, _ := procSelectObject.Call(memDC, hbm)
	defer procSelectObject.Call(memDC, old)
	ret, _, err := procPrintWindow.Call(desktop, memDC, pwRenderFull)
	if ret == 0 {
		ret, _, err = procPrintWindow.Call(desktop, memDC, 0)
		if ret == 0 {
			return nil, fmt.Errorf("PrintWindow: %v", err)
		}
	}
	return dibToRGBA(hdc, hbm, width, height)
}

func dibToRGBA(hdc, hbm uintptr, width, height int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(width)
	bi.Header.Height = -int32(height)
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB
	ret, _, err := procGetDIBits.Call(
		hdc, hbm, 0, uintptr(height),
		uintptr(unsafe.Pointer(&img.Pix[0])),
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits: %v", err)
	}
	for i := 0; i < width*height; i++ {
		off := i * 4
		img.Pix[off+0], img.Pix[off+2] = img.Pix[off+2], img.Pix[off+0]
		img.Pix[off+3] = 255
	}
	return img, nil
}

func downscale(full *image.RGBA, scale int) *image.RGBA {
	if scale <= 1 {
		return full
	}
	width := full.Bounds().Dx()
	height := full.Bounds().Dy()
	outW, outH := width/scale, height/scale
	if outW < 1 || outH < 1 {
		return full
	}
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for oy := 0; oy < outH; oy++ {
		sy := oy * scale
		for ox := 0; ox < outW; ox++ {
			sx := ox * scale
			si := (sy*width + sx) * 4
			di := (oy*outW + ox) * 4
			copy(out.Pix[di:di+4], full.Pix[si:si+4])
		}
	}
	return out
}
