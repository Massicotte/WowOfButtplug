package main

import (
	"bufio"
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
	"sync/atomic"
	"syscall"
	"time"
)

const (
	serverURL   = "localhost:12345"
	serverPath  = "/buttplug"
	pollEvery   = 50 * time.Millisecond
	flashMinGap = 300 * time.Millisecond
	vibeTime    = 500 * time.Millisecond
)

// ------------------------------------------------------------ websocket ----

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
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

	r := bufio.NewReader(conn)
	status, err := r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, errors.New("websocket handshake failed: " + strings.TrimSpace(status))
	}
	// Drain the remaining headers up to the empty line.
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return &wsConn{conn: conn, r: r}, nil
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
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		opcode := h[0] & 0x0F
		masked := h[1]&0x80 != 0
		plen := uint64(h[1] & 0x7F)
		switch plen {
		case 126:
			var ext [2]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			plen = uint64(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			if _, err := io.ReadFull(w.r, ext[:]); err != nil {
				return nil, err
			}
			plen = binary.BigEndian.Uint64(ext[:])
		}
		if plen > 1<<20 {
			return nil, errors.New("frame too large")
		}
		payload := make([]byte, plen)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return nil, err
		}
		if masked {
			var mk [4]byte
			if _, err := io.ReadFull(w.r, mk[:]); err != nil {
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
	index      int
	name       string
	featureCnt int
}

type buttplugClient struct {
	ws       *wsConn
	id       int
	mu       sync.Mutex
	devices  map[int]*device
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

// send marshals one buttplug message in the wire format the server expects:
// a JSON array containing a single object keyed by the message name, with
// the Id inside the contents: [{"VibrateCmd":{"Id":1,...}}].
func (c *buttplugClient) send(name string, fields map[string]any) error {
	c.mu.Lock()
	c.id++
	id := c.id
	c.mu.Unlock()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["Id"] = id
	data, err := json.Marshal([]any{map[string]any{name: fields}})
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
	if err := c.send("RequestDeviceList", nil); err != nil {
		return err
	}
	return c.send("StartScanning", nil)
}

// handle parses one server payload: a JSON array of objects, each keyed by
// the message name with the contents as value: [{"ServerInfo":{...}}].
func (c *buttplugClient) handle(data []byte) {
	var envelope []json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		fmt.Printf("Unparseable server message: %s\n", data)
		return
	}
	for _, raw := range envelope {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil || len(obj) != 1 {
			continue
		}
		for name, contents := range obj {
			c.handleOne(name, contents)
		}
	}
}

func (c *buttplugClient) handleOne(name string, payload json.RawMessage) {
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
		c.mu.Lock()
		for _, d := range list {
			cnt := featureCount(d.DeviceMessages, "VibrateCmd")
			c.devices[d.DeviceIndex] = &device{index: d.DeviceIndex, name: d.DeviceName, featureCnt: cnt}
			if cnt > 0 {
				fmt.Printf("Vibration device found: %s (%d motor(s))\n", d.DeviceName, cnt)
			} else {
				fmt.Printf("Device found (no vibration): %s\n", d.DeviceName)
			}
		}
		c.mu.Unlock()
	case "DeviceRemoved":
		var dr struct {
			DeviceIndex int `json:"DeviceIndex"`
		}
		if json.Unmarshal(payload, &dr) == nil {
			c.mu.Lock()
			delete(c.devices, dr.DeviceIndex)
			c.mu.Unlock()
		}
	case "ScanningFinished":
		_ = c.send("StopScanning", nil)
		select {
		case <-c.stopPoke:
		default:
			close(c.stopPoke)
		}
	case "Error":
		var e struct {
			ErrorMessage string `json:"ErrorMessage"`
		}
		_ = json.Unmarshal(payload, &e)
		fmt.Printf("Server error: %s\n", e.ErrorMessage)
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
		if f, ok := m[msg]; ok {
			if f.FeatureCount > 0 {
				return f.FeatureCount
			}
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
		if err := c.send("Ping", nil); err != nil {
			return
		}
	}
}

// snapshotDevices returns a copy of the device list. Callers must not hold mu.
func (c *buttplugClient) snapshotDevices() []*device {
	c.mu.Lock()
	defer c.mu.Unlock()
	list := make([]*device, 0, len(c.devices))
	for _, d := range c.devices {
		list = append(list, d)
	}
	return list
}

func (c *buttplugClient) hasVibrationDevice() bool {
	for _, d := range c.snapshotDevices() {
		if d.featureCnt > 0 {
			return true
		}
	}
	return false
}

func (c *buttplugClient) vibrateAll(speed float64) {
	for _, d := range c.snapshotDevices() {
		if d.featureCnt == 0 {
			continue
		}
		speeds := make([]map[string]any, d.featureCnt)
		for i := range speeds {
			speeds[i] = map[string]any{"Index": i, "Speed": speed}
		}
		_ = c.send("VibrateCmd", map[string]any{"DeviceIndex": d.index, "Speeds": speeds})
	}
}

func (c *buttplugClient) stopAll() {
	for _, d := range c.snapshotDevices() {
		_ = c.send("StopDeviceCmd", map[string]any{"DeviceIndex": d.index})
	}
}

// ----------------------------------------------------------------- main ----

func main() {
	fmt.Println("WowOfButtplug - Butthurt bridge for WoW: Forever")
	fmt.Println("Waiting for the Intiface Central server on ws://" + serverURL + serverPath + " ...")

	var ws *wsConn
	var err error
	for attempt := 1; attempt <= 60; attempt++ {
		ws, err = dialWebSocket(serverURL, serverPath)
		if err == nil {
			break
		}
		if attempt%5 == 0 {
			fmt.Printf("Still waiting for the server... (attempt %d, last error: %v)\n", attempt, err)
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

	if !client.hasVibrationDevice() {
		fmt.Println("No vibration-capable device found.")
		fmt.Println("Pair your toy in Intiface Central, then run this program again.")
		client.stopAll()
		os.Exit(1)
	}

	fmt.Println("Ready. Keep the WoW window in the foreground and take some damage!")
	fmt.Println("Press Ctrl+C to stop.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	var lastFlash time.Time
	var flashGen uint64
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
			red := redPixelNearTop()
			if red && !wasRed && time.Since(lastFlash) >= flashMinGap {
				lastFlash = time.Now()
				gen := atomic.AddUint64(&flashGen, 1)
				client.vibrateAll(1.0)
				time.AfterFunc(vibeTime, func() {
					// Only stop if no newer flash happened meanwhile.
					if atomic.LoadUint64(&flashGen) == gen {
						client.vibrateAll(0.0)
					}
				})
			}
			wasRed = red
		}
	}
}
