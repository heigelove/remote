//go:build !windows

package main

func setCaptureMapping(originX, originY, srcW, srcH, imgW, imgH int) {}

func mapImageToScreen(imgX, imgY int) (int, int) { return imgX, imgY }
