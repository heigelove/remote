package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
)

func captureJPEG(quality int) ([]byte, error) {
	if quality <= 0 {
		quality = streamJPEGQuality()
	}
	if lockScreenCaptureNeeded() {
		if jpg, err := pullLockCaptureJPEG(); err == nil && len(jpg) >= 100 {
			return jpg, nil
		}
	}
	img, err := captureScreen()
	if err != nil {
		if lockScreenCaptureNeeded() {
			if jpg, err2 := pullLockCaptureJPEG(); err2 == nil && len(jpg) >= 100 {
				return jpg, nil
			}
		}
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	if buf.Len() < 100 {
		return nil, fmt.Errorf("jpeg too small: %d", buf.Len())
	}
	return buf.Bytes(), nil
}
