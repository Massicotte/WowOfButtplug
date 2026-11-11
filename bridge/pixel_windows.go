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

// redPixelNearTop scans a small grid around the top-left corner of the
// foreground window's client area, where the addon draws its pixel.
//
// The pixel is read from the SCREEN device context (GetDC(0)), not the
// window's: WoW renders with DirectX, and the window DC does not contain
// the rendered game image (GetPixel would return black). The screen DC
// holds what is actually displayed, like the CopyFromScreen call the
// original Butthurt.exe used.
func redPixelNearTop() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var r rect
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 || r.Right <= 0 || r.Bottom <= 0 {
		return false
	}
	p := point{0, 0}
	ret, _, _ = procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	if ret == 0 {
		return false
	}
	hdc, _, _ := procGetDC.Call(0) // entire screen
	if hdc == 0 {
		return false
	}
	defer procReleaseDC.Call(0, hdc)
	for y := p.Y; y < p.Y+10; y++ {
		for x := p.X; x < p.X+10; x++ {
			cr, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
			if cr == 0xFFFFFFFF { // CLR_INVALID
				continue
			}
			// COLORREF: 0x00BBGGRR
			red := uint8(cr & 0xFF)
			g := uint8((cr >> 8) & 0xFF)
			b := uint8((cr >> 16) & 0xFF)
			if red > 200 && g < 50 && b < 50 {
				return true
			}
		}
	}
	return false
}
