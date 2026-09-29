package main

import (
	"image"
	"strings"
	"sync/atomic"
)

var (
	streamMaxW atomic.Int32
	streamMaxH atomic.Int32
	streamJPEG atomic.Int32
)

func init() {
	setStreamQuality("1080p")
}

func normalizeQuality(q string) string {
	switch strings.ToLower(strings.TrimSpace(q)) {
	case "native", "original", "source", "raw":
		return "native"
	case "720p", "hd", "720":
		return "720p"
	case "480p", "sd", "480":
		return "480p"
	default:
		return "1080p"
	}
}

func setStreamQuality(q string) {
	q = normalizeQuality(q)
	switch q {
	case "native":
		streamMaxW.Store(0)
		streamMaxH.Store(0)
		streamJPEG.Store(55)
	case "720p":
		streamMaxW.Store(1280)
		streamMaxH.Store(720)
		streamJPEG.Store(65)
	case "480p":
		streamMaxW.Store(854)
		streamMaxH.Store(480)
		streamJPEG.Store(70)
	default:
		streamMaxW.Store(1920)
		streamMaxH.Store(1080)
		streamJPEG.Store(60)
	}
	LogInfo("stream quality=%s max=%dx%d jpeg=%d", q, streamMaxW.Load(), streamMaxH.Load(), streamJPEG.Load())
}

func streamJPEGQuality() int {
	q := int(streamJPEG.Load())
	if q < 40 || q > 95 {
		return 60
	}
	return q
}

func fitStreamSize(srcW, srcH int) (int, int) {
	maxW := int(streamMaxW.Load())
	maxH := int(streamMaxH.Load())
	if maxW <= 0 || maxH <= 0 || srcW <= 0 || srcH <= 0 {
		return srcW, srcH
	}
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	var outW, outH int
	if srcW*maxH > srcH*maxW {
		outW = maxW
		outH = srcH * maxW / srcW
	} else {
		outH = maxH
		outW = srcW * maxH / srcH
	}
	if outW < 1 {
		outW = 1
	}
	if outH < 1 {
		outH = 1
	}
	return outW, outH
}

func applyStreamSize(img *image.RGBA, originX, originY int) *image.RGBA {
	if img == nil {
		return nil
	}
	srcW, srcH := img.Bounds().Dx(), img.Bounds().Dy()
	outW, outH := fitStreamSize(srcW, srcH)
	setCaptureMapping(originX, originY, srcW, srcH, outW, outH)
	if outW == srcW && outH == srcH {
		return img
	}
	return resizeNearest(img, outW, outH)
}

func resizeNearest(src *image.RGBA, outW, outH int) *image.RGBA {
	inW, inH := src.Bounds().Dx(), src.Bounds().Dy()
	if outW == inW && outH == inH {
		return src
	}
	if outW < 1 || outH < 1 || inW < 1 || inH < 1 {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		sy := y * inH / outH
		for x := 0; x < outW; x++ {
			sx := x * inW / outW
			si := (sy*inW + sx) * 4
			di := (y*outW + x) * 4
			copy(out.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return out
}
