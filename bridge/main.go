package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	serverURL   = "localhost:12345"
	serverPath  = "/buttplug"
	pollEvery   = 50 * time.Millisecond
	flashMinGap = 300 * time.Millisecond
	vibeTime    = 500 * time.Millisecond
)

// ---------------------------------------------------------------- Win32 ----

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

// clientTopLeft returns the screen coordinates of the top-left corner of the
// foreground window's client area (where the addon draws its pixel).
func clientTopLeft() (int32, int32, bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0, 0, false
	}
	var r rect
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 || r.Right <= 0 || r.Bottom <= 0 {
		return 0, 0, false
	}
	p := point{0, 0}
	ret, _, _ = procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	if ret == 0 {
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// redPixelAt reports whether the pixel at (x, y) is the red the addon flashes.
func redPixelAt(x, y int32) bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		return false
	}
	defer procReleaseDC.Call(hwnd, hdc)
	cr, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
	// COLORREF: 0x00BBGGRR
	r := uint8(cr & 0xFF)
	g := uint8((cr >> 8) & 0xFF)
	b := uint8((cr >> 16) & 0xFF)
	return r > 200 && g < 50 && b < 50
}

// ------------------------------------------------------------ websocket ----

type wsConn struct {
	conn net.Conn
	wmu  sync.Mutex
}

func dialWebSocket(host, path string) (*wsConn, error) {
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var keyBytes [16]byte
	_, _ = rand.Read(keyBytes[:])
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp := string(buf[:n])
	if !strings.HasPrefix(resp, "HTTP/1.1 101") && !strings.HasPrefix(resp, "HTTP/1.0 101") {
		conn.Close()
		return nil, errors.New("websocket handshake failed: " + strings.TrimSpace(strings.SplitN(resp, "\r\n", 2)[0]))
	}
	return &wsConn{conn: conn}, nil
}

func (w *wsConn) sendText(data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.writeFrame(0x1, data)
}

func (w *wsConn) writeFrame(opcode byte, data []byte) error {
	var header []byte
	header = append(header, 0x80|opcode) // FIN + opcode
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	n := len(data)
	switch {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n <= 0xFFFF:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(n))
		header = append(header, append([]byte{0x80 | 127}, ext...)...)
	}
	header = append(header, mask...)
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = data[i] ^ mask[i%4]
	}
	if _, err := w.conn.Write(append(header, masked...)); err != nil {
		return err
	}
	return nil
}

// readMessage returns the next complete text message payload.
func (w *wsConn) readMessage() ([]byte, error) {
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.conn, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0F
		masked := h[1]&0x80 != 0
		plen := uint64(h[1] & 0x7F)
		switch plen {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.conn, ext[:]); err != nil {
				return nil, err
			}
			plen = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.conn, ext[:]); err != nil {
				return nil, err
			}
			plen = binary.BigEndian.Uint64(ext[:])
		}
		if plen > 1<<20 {
			return nil, errors.New("frame too large")
		}
		payload := make([]byte, plen)
		if _, err := io.ReadFull(w.conn, payload); err != nil {
			return nil, err
		}
		if masked {
			var mk [4]byte
			if _, err := io.ReadFull(w.conn, mk[:]); err != nil {
				return nil, err
			}
			for i := range payload {
				payload[i] ^= mk[i%4]
			}
		}
		switch opcode {
		case 0x8: // close
			_ = w.writeFrame(0x8, payload)
			return nil, io.EOF
		case 0x9: // ping -> pong
			if err := w.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA: // pong
		case 0x1, 0x2, 0x0:
			if fin {
				return payload, nil
			}
		}
	}
}

// ------------------------------------------------------------- buttplug ----

type device struct {
	name       string
	featureCnt int
}

type buttplugClient struct {
	ws       *wsConn
	id       int
	mu       sync.Mutex
	devices  map[int]*device
	listCB   func(name string)
	stopPoke chan struct{}
}

type serverInfoPayload struct {
	MessageVersion int  `json:"MessageVersion"`
	MaxPingTime    uint `json:"MaxPingTime"`
}

type deviceAddedPayload struct {
	DeviceIndex    int             `json:"DeviceIndex"`
	DeviceName     string          `json:"DeviceName"`
	DeviceMessages json.RawMessage `json:"DeviceMessages"`
}

func newButtplugClient(ws *wsConn) *buttplugClient {
	return &buttplugClient{ws: ws, devices: map[int]*device{}, stopPoke: make(chan struct{})}
}

func (c *buttplugClient) send(name string, payload any) error {
	c.mu.Lock()
	c.id++
	id := c.id
	c.mu.Unlock()
	msg := []any{id, name, payload}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.ws.sendText(data)
}

func (c *buttplugClient) connect() error {
	if err := c.send("RequestServerInfo", map[string]any{
		"ClientName":     "WowOfButtplug",
		"MessageVersion": 3,
	}); err != nil {
		return err
	}
	if err := c.send("RequestDeviceList", map[string]any{}); err != nil {
		return err
	}
	return c.send("StartScanning", map[string]any{})
}

// handle parses one server message. Returns true once scanning finished.
func (c *buttplugClient) handle(data []byte) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) < 2 {
		return
	}
	var name string
	if err := json.Unmarshal(arr[1], &name); err != nil {
		return
	}
	var payload json.RawMessage
	if len(arr) >= 3 {
		payload = arr[2]
	}

	switch name {
	case "ServerInfo":
		var si serverInfoPayload
		_ = json.Unmarshal(payload, &si)
		fmt.Printf("Connected to %s (buttplug v%d)\n", serverInfoName(payload), si.MessageVersion)
		if si.MaxPingTime > 0 && si.MessageVersion < 3 {
			go c.pinger(time.Duration(si.MaxPingTime) * time.Millisecond / 3)
		}
	case "DeviceAdded", "DeviceList":
		var list []deviceAddedPayload
		if name == "DeviceList" {
			var dl struct {
				Devices []deviceAddedPayload `json:"Devices"`
			}
			if json.Unmarshal(payload, &dl) == nil {
				list = dl.Devices
			}
		} else {
			var d deviceAddedPayload
			if json.Unmarshal(payload, &d) == nil {
				list = []deviceAddedPayload{d}
			}
		}
		for _, d := range list {
			cnt := featureCount(d.DeviceMessages, "VibrateCmd")
			c.devices[d.DeviceIndex] = &device{name: d.DeviceName, featureCnt: cnt}
			if cnt > 0 {
				fmt.Printf("Vibration device found: %s (%d motor(s))\n", d.DeviceName, cnt)
			} else {
				fmt.Printf("Device found (no vibration): %s\n", d.DeviceName)
			}
		}
	case "DeviceRemoved":
		var dr struct {
			DeviceIndex int `json:"DeviceIndex"`
		}
		if json.Unmarshal(payload, &dr) == nil {
			delete(c.devices, dr.DeviceIndex)
		}
	case "ScanningFinished":
		_ = c.send("StopScanning", map[string]any{})
		select {
		case <-c.stopPoke:
		default:
			close(c.stopPoke)
		}
	case "Error":
		fmt.Printf("Server error: %s\n", string(payload))
	}
}

func serverInfoName(payload json.RawMessage) string {
	var si struct {
		ServerName string `json:"ServerName"`
	}
	_ = json.Unmarshal(payload, &si)
	if si.ServerName == "" {
		return "buttplug server"
	}
	return si.ServerName
}

// featureCount handles both v3 ({"VibrateCmd":{"FeatureCount":1}}) and
// v1/v2 (["VibrateCmd"]) DeviceMessages formats.
func featureCount(raw json.RawMessage, msg string) int {
	var m map[string]struct {
		FeatureCount int `json:"FeatureCount"`
	}
	if json.Unmarshal(raw, &m) == nil {
		if f, ok := m[msg]; ok && f.FeatureCount > 0 {
			return f.FeatureCount
		}
		if _, ok := m[msg]; ok {
			return 1
		}
		return 0
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		for _, s := range arr {
			if s == msg {
				return 1
			}
		}
	}
	return 0
}

func (c *buttplugClient) pinger(every time.Duration) {
	if every < time.Second {
		every = time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		if err := c.send("Ping", map[string]any{}); err != nil {
			return
		}
	}
}

func (c *buttplugClient) vibrateAll(speed float64) {
	c.mu.Lock()
	devices := make([]*device, 0, len(c.devices))
	for _, d := range c.devices {
		devices = append(devices, d)
	}
	c.mu.Unlock()
	for _, d := range devices {
		if d.featureCnt == 0 {
			continue
		}
		speeds := make([]map[string]any, d.featureCnt)
		for i := range speeds {
			speeds[i] = map[string]any{"Index": i, "Speed": speed}
		}
		_ = c.send("VibrateCmd", map[string]any{"DeviceIndex": indexOf(c.devices, d), "Speeds": speeds})
	}
}

func (c *buttplugClient) stopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for idx := range c.devices {
		_ = c.send("StopDeviceCmd", map[string]any{"DeviceIndex": idx})
	}
}

func indexOf(m map[int]*device, d *device) int {
	for i, v := range m {
		if v == d {
			return i
		}
	}
	return 0
}

// ----------------------------------------------------------------- main ----

func main() {
	fmt.Println("WowOfButtplug - Butthurt bridge for WoW: Forever")
	fmt.Println("Waiting for the Intiface Central server on ws://" + serverURL + serverPath + " ...")

	var ws *wsConn
	var err error
	for attempt := 0; attempt < 60; attempt++ {
		ws, err = dialWebSocket(serverURL, serverPath)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		fmt.Println("Could not reach the buttplug server:", err)
		fmt.Println("Start Intiface Central, click 'Start Server', then run this program again.")
		os.Exit(1)
	}
	defer ws.conn.Close()

	client := newButtplugClient(ws)
	if err := client.connect(); err != nil {
		fmt.Println("Handshake failed:", err)
		os.Exit(1)
	}

	msgs := make(chan []byte, 64)
	go func() {
		for {
			data, err := ws.readMessage()
			if err != nil {
				close(msgs)
				return
			}
			msgs <- data
		}
	}()

	go func() {
		for data := range msgs {
			client.handle(data)
		}
		fmt.Println("Connection to the buttplug server lost.")
		client.stopAll()
		os.Exit(1)
	}()

	// Give the scan some time to find devices.
	select {
	case <-client.stopPoke:
	case <-time.After(15 * time.Second):
	}

	hasDevice := false
	client.mu.Lock()
	for _, d := range client.devices {
		if d.featureCnt > 0 {
			hasDevice = true
		}
	}
	client.mu.Unlock()
	if !hasDevice {
		fmt.Println("No vibration-capable device found.")
		fmt.Println("Pair your toy in Intiface Central, then run this program again.")
		os.Exit(1)
	}

	fmt.Println("Ready. Keep the WoW window in the foreground and take some damage!")
	fmt.Println("Press Ctrl+C to stop.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	var lastFlash time.Time
	var wasRed bool
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	for {
		select {
		case <-sig:
			client.stopAll()
			fmt.Println("Stopped.")
			return
		case <-tick.C:
			x, y, ok := clientTopLeft()
			if !ok {
				continue
			}
			red := redPixelAt(x, y)
			if red && !wasRed && time.Since(lastFlash) >= flashMinGap {
				lastFlash = time.Now()
				client.vibrateAll(1.0)
				time.AfterFunc(vibeTime, func() { client.vibrateAll(0.0) })
			}
			wasRed = red
		}
	}
}
