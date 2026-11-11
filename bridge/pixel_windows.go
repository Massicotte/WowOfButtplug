//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetPixel            = gdi32.NewProc("GetPixel")
)

type rect struct{ Left, Top, Right, Bottom int32 }
type point struct{ X, Y int32 }

// samplePixels reads the addon's two blocks at the top-left corner of the
// foreground window's client area: a colored block encoding the event type
// and a gray block encoding the intensity (0-1).
//
// The pixels are read from the SCREEN device context (GetDC(0)), not the
// window's: WoW renders with DirectX, and the window DC does not contain
// the rendered game image.
func samplePixels() (event string, intensity float64) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", 0
	}
	var r rect
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 || r.Right <= 0 || r.Bottom <= 0 {
		return "", 0
	}
	p := point{0, 0}
	ret, _, _ = procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	if ret == 0 {
		return "", 0
	}
	hdc, _, _ := procGetDC.Call(0) // entire screen
	if hdc == 0 {
		return "", 0
	}
	defer procReleaseDC.Call(0, hdc)

	counts := map[string]int{}
	graySum, grayCnt := 0, 0
	for y := p.Y; y < p.Y+10; y++ {
		for x := p.X; x < p.X+20; x++ {
			cr, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
			if cr == 0xFFFFFFFF { // CLR_INVALID
				continue
			}
			// COLORREF: 0x00BBGGRR
			red := int(cr & 0xFF)
			green := int((cr >> 8) & 0xFF)
			blue := int((cr >> 16) & 0xFF)
			switch {
			case red > 180 && green < 70 && blue < 70:
				counts["damage"]++
			case green > 180 && red < 70 && blue < 70:
				counts["heal"]++
			case red > 180 && green > 180 && blue < 70:
				counts["loot"]++
			case green > 180 && blue > 180 && red < 70:
				counts["mining"]++
			case red > 180 && blue > 180 && green < 70:
				counts["whisper"]++
			case blue > 180 && red < 70 && green < 70:
				counts["guild"]++
			case abs(red-green) < 25 && abs(green-blue) < 25 && red >= 40:
				graySum += red
				grayCnt++
			}
		}
	}

	best, bestCnt := "", 0
	for name, cnt := range counts {
		if cnt > bestCnt {
			best, bestCnt = name, cnt
		}
	}
	if best == "" {
		return "", 0
	}
	intensity = 0.5
	if grayCnt > 0 {
		intensity = float64(graySum) / float64(grayCnt) / 255
	}
	return best, intensity
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
