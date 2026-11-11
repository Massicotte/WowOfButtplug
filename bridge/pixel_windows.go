//go:build windows

package main

import (
	"syscall"
)

// ---------------------------------------------------------------- Win32 ----

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetPixel            = gdi32.NewProc("GetPixel")
)

// redPixelNearTop scans a small grid at the top-left corner of the foreground
// window's client area for the red pixel the addon flashes. GetDC returns a
// client-area DC, so GetPixel takes client coordinates (0,0 = top-left).
func redPixelNearTop() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		return false
	}
	defer procReleaseDC.Call(hwnd, hdc)
	for y := int32(0); y < 8; y++ {
		for x := int32(0); x < 8; x++ {
			cr, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
			if cr == 0xFFFFFFFF { // CLR_INVALID
				continue
			}
			// COLORREF: 0x00BBGGRR
			r := uint8(cr & 0xFF)
			g := uint8((cr >> 8) & 0xFF)
			b := uint8((cr >> 16) & 0xFF)
			if r > 200 && g < 50 && b < 50 {
				return true
			}
		}
	}
	return false
}
