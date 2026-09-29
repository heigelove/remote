//go:build windows

package main

import (
	"sync/atomic"
)

// Updated on each successful capture so mouse coords map correctly.
var (
	capOriginX atomic.Int32
	capOriginY atomic.Int32
	capSrcW    atomic.Int32
	capSrcH    atomic.Int32
	capImgW    atomic.Int32
	capImgH    atomic.Int32
)

func setCaptureMapping(originX, originY, srcW, srcH, imgW, imgH int) {
	if srcW < 1 {
		srcW = 1
	}
	if srcH < 1 {
		srcH = 1
	}
	if imgW < 1 {
		imgW = srcW
	}
	if imgH < 1 {
		imgH = srcH
	}
	capOriginX.Store(int32(originX))
	capOriginY.Store(int32(originY))
	capSrcW.Store(int32(srcW))
	capSrcH.Store(int32(srcH))
	capImgW.Store(int32(imgW))
	capImgH.Store(int32(imgH))
}

func mapImageToScreen(imgX, imgY int) (int, int) {
	iw := int(capImgW.Load())
	ih := int(capImgH.Load())
	sw := int(capSrcW.Load())
	sh := int(capSrcH.Load())
	if iw < 1 {
		iw = 1
	}
	if ih < 1 {
		ih = 1
	}
	if sw < 1 {
		sw = iw
	}
	if sh < 1 {
		sh = ih
	}
	return imgX*sw/iw + int(capOriginX.Load()), imgY*sh/ih + int(capOriginY.Load())
}
