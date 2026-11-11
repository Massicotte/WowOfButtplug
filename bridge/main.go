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
	"math"
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

// vibeFeature is a v4 feature we can vibrate.
type vibeFeature struct {
	featureIndex int
	maxValue     int
}

type device struct {
	index int
	name  string
	// Protocol v3 capabilities.
	vibrateCnt    int // motors exposed via VibrateCmd
	scalarVibeCnt int // vibrate actuators exposed via ScalarCmd
	// Protocol v4 capabilities.
	v4Vibes []vibeFeature
}

type buttplugClient struct {
	ws       *wsConn
	id       int
	mu       sync.Mutex
	devices  map[int]*device
	testSent bool
	version  int // negotiated protocol major version (3 or 4)
}

var debug = os.Getenv("WOWOFBUTTPLUG_DEBUG") == "1"

func newButtplugClient(ws *wsConn) *buttplugClient {
	return &buttplugClient{ws: ws, devices: map[int]*device{}}
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
	if debug {
		fmt.Printf("[debug] -> %s\n", data)
	}
	return c.ws.sendText(data)
}

// connect starts the handshake. We ask for protocol v4 first; if the server
// rejects it we retry with v3 (see the Error handler in handleOne).
func (c *buttplugClient) connect() error {
	return c.send("RequestServerInfo", map[string]any{
		"ClientName":           "WowOfButtplug",
		"ProtocolVersionMajor": 4,
		"ProtocolVersionMinor": 0,
	})
}

func (c *buttplugClient) fallbackV3() error {
	c.mu.Lock()
	c.version = 3
	c.mu.Unlock()
	return c.send("RequestServerInfo", map[string]any{
		"ClientName":     "WowOfButtplug",
		"MessageVersion": 3,
	})
}

// handle parses one server payload: a JSON array of objects, each keyed by
// the message name with the contents as value: [{"ServerInfo":{...}}].
func (c *buttplugClient) handle(data []byte) {
	if debug {
		fmt.Printf("[debug] <- %s\n", data)
	}
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
		var si struct {
			ServerName           string `json:"ServerName"`
			MessageVersion       int    `json:"MessageVersion"`       // v3 and earlier
			ProtocolVersionMajor int    `json:"ProtocolVersionMajor"` // v4
			MaxPingTime          uint   `json:"MaxPingTime"`
		}
		_ = json.Unmarshal(payload, &si)
		version := si.ProtocolVersionMajor
		if version == 0 {
			version = si.MessageVersion
		}
		c.mu.Lock()
		c.version = version
		c.mu.Unlock()
		fmt.Printf("Connected to %s (buttplug v%d)\n", si.ServerName, version)
		if si.MaxPingTime > 0 && version < 4 {
			go c.pinger(time.Duration(si.MaxPingTime) * time.Millisecond / 3)
		}
		// Handshake answered: now request the device list and start scanning.
		_ = c.send("RequestDeviceList", nil)
		_ = c.send("StartScanning", nil)
	case "DeviceAdded", "DeviceList":
		// v3 sends DeviceAdded (single device) and DeviceList (array);
		// v4 only sends DeviceList with Devices as a map of index -> device.
		list := parseDeviceList(name, payload)
		for _, d := range list {
			c.registerDevice(d)
		}
	case "DeviceRemoved":
		var dr struct {
			DeviceIndex int `json:"DeviceIndex"`
		}
		if json.Unmarshal(payload, &dr) == nil {
			c.mu.Lock()
			delete(c.devices, dr.DeviceIndex)
			c.mu.Unlock()
			fmt.Println("Device removed.")
		}
	case "ScanningFinished":
		_ = c.send("StopScanning", nil)
	case "Error":
		var e struct {
			ErrorMessage string `json:"ErrorMessage"`
		}
		_ = json.Unmarshal(payload, &e)
		c.mu.Lock()
		handshakePending := c.version == 0
		c.mu.Unlock()
		if handshakePending {
			// Server refused our v4 handshake: retry with v3.
			fmt.Printf("Server refused protocol v4 (%s), retrying with v3...\n", e.ErrorMessage)
			_ = c.fallbackV3()
			return
		}
		fmt.Printf("Server error: %s\n", e.ErrorMessage)
	case "Ok":
		// Nothing to do.
	}
}

// registerDevice stores a device and sends the one-time test vibration.
func (c *buttplugClient) registerDevice(d *device) {
	c.mu.Lock()
	c.devices[d.index] = d
	test := !c.testSent
	c.testSent = true
	vibes := len(d.v4Vibes) + d.vibrateCnt + d.scalarVibeCnt
	c.mu.Unlock()
	if vibes > 0 {
		fmt.Printf("Vibration device found: %s (%d motor(s))\n", d.name, vibes)
		if test {
			// Short test buzz so the user can tell the chain works.
			fmt.Println("Sending a short test vibration (0.3s)...")
			c.vibrateAll(1.0)
			time.AfterFunc(300*time.Millisecond, func() { c.vibrateAll(0.0) })
			time.AfterFunc(time.Second, func() { c.vibrateAll(0.0) })
		}
	} else {
		fmt.Printf("Device found (no vibration support): %s\n", d.name)
		if debug {
			fmt.Printf("[debug] raw device: %+v\n", d)
		}
	}
}

// parseDeviceList extracts devices from a DeviceAdded or DeviceList payload,
// handling both the v3 shape (array of devices with DeviceMessages) and the
// v4 shape (map of index -> device with DeviceFeatures).
func parseDeviceList(name string, payload json.RawMessage) []*device {
	var out []*device
	if name == "DeviceAdded" {
		if d := parseV3Device(payload); d != nil {
			out = append(out, d)
		}
		return out
	}
	var dl struct {
		Devices json.RawMessage `json:"Devices"`
	}
	if json.Unmarshal(payload, &dl) != nil {
		return out
	}
	// v3: array of devices.
	var arr []json.RawMessage
	if json.Unmarshal(dl.Devices, &arr) == nil {
		for _, raw := range arr {
			if d := parseV3Device(raw); d != nil {
				out = append(out, d)
			}
		}
		return out
	}
	// v4: map of index -> device.
	var m map[string]json.RawMessage
	if json.Unmarshal(dl.Devices, &m) == nil {
		for _, raw := range m {
			if d := parseV4Device(raw); d != nil {
				out = append(out, d)
			}
		}
	}
	return out
}

func parseV3Device(raw json.RawMessage) *device {
	var d struct {
		DeviceIndex    int             `json:"DeviceIndex"`
		DeviceName     string          `json:"DeviceName"`
		DeviceMessages json.RawMessage `json:"DeviceMessages"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	vibCnt, scalCnt := vibrationFeatures(d.DeviceMessages)
	return &device{index: d.DeviceIndex, name: d.DeviceName, vibrateCnt: vibCnt, scalarVibeCnt: scalCnt}
}

func parseV4Device(raw json.RawMessage) *device {
	var d struct {
		DeviceIndex    int    `json:"DeviceIndex"`
		DeviceName     string `json:"DeviceName"`
		DeviceFeatures map[string]struct {
			FeatureIndex int `json:"FeatureIndex"`
			Output       map[string]struct {
				Value []int `json:"Value"`
			} `json:"Output"`
		} `json:"DeviceFeatures"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	dev := &device{index: d.DeviceIndex, name: d.DeviceName}
	for _, f := range d.DeviceFeatures {
		if out, ok := f.Output["Vibrate"]; ok {
			max := 1
			if len(out.Value) >= 2 && out.Value[1] > 0 {
				max = out.Value[1]
			}
			dev.v4Vibes = append(dev.v4Vibes, vibeFeature{featureIndex: f.FeatureIndex, maxValue: max})
		}
	}
	return dev
}

// vibrationFeatures extracts how a device can vibrate from its v3
// DeviceMessages field:
//   - v3: {"VibrateCmd": [{"StepCount": 20}]} or {"ScalarCmd": [{"ActuatorType": "Vibrate", "StepCount": 20}]}
//   - v1/v2: ["VibrateCmd"] or {"VibrateCmd": {"FeatureCount": 2}}
func vibrationFeatures(raw json.RawMessage) (vibrateCnt, scalarVibeCnt int) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		if v, ok := m["VibrateCmd"]; ok {
			vibrateCnt = countFeatures(v, "")
		}
		if v, ok := m["ScalarCmd"]; ok {
			scalarVibeCnt = countFeatures(v, "Vibrate")
		}
		return vibrateCnt, scalarVibeCnt
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		for _, s := range arr {
			if s == "VibrateCmd" {
				return 1, 0
			}
		}
	}
	return 0, 0
}

// countFeatures counts feature descriptors in a v3 message attribute:
// either an array of attribute objects (one per feature) or a single
// object with FeatureCount. actuator filters ScalarCmd descriptors;
// empty accepts all.
func countFeatures(raw json.RawMessage, actuator string) int {
	var obj struct {
		FeatureCount int    `json:"FeatureCount"`
		ActuatorType string `json:"ActuatorType"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		if actuator == "" || obj.ActuatorType == actuator {
			if obj.FeatureCount > 0 {
				return obj.FeatureCount
			}
			return 1
		}
		return 0
	}
	var arr []struct {
		FeatureCount int    `json:"FeatureCount"`
		ActuatorType string `json:"ActuatorType"`
	}
	total := 0
	if json.Unmarshal(raw, &arr) == nil {
		for _, f := range arr {
			if actuator == "" || f.ActuatorType == actuator {
				if f.FeatureCount > 0 {
					total += f.FeatureCount
				} else {
					total++
				}
			}
		}
	}
	return total
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
		if d.vibrateCnt > 0 || d.scalarVibeCnt > 0 || len(d.v4Vibes) > 0 {
			return true
		}
	}
	return false
}

// vibrateAll sets every vibration-capable device to speed (0.0-1.0), using
// the command matching the negotiated protocol version.
func (c *buttplugClient) vibrateAll(speed float64) {
	for _, d := range c.snapshotDevices() {
		switch {
		case d.vibrateCnt > 0:
			speeds := make([]map[string]any, d.vibrateCnt)
			for i := range speeds {
				speeds[i] = map[string]any{"Index": i, "Speed": speed}
			}
			_ = c.send("VibrateCmd", map[string]any{"DeviceIndex": d.index, "Speeds": speeds})
		case d.scalarVibeCnt > 0:
			scalars := make([]map[string]any, d.scalarVibeCnt)
			for i := range scalars {
				scalars[i] = map[string]any{"Index": i, "Scalar": speed, "ActuatorType": "Vibrate"}
			}
			_ = c.send("ScalarCmd", map[string]any{"DeviceIndex": d.index, "Scalars": scalars})
		case len(d.v4Vibes) > 0:
			for _, f := range d.v4Vibes {
				value := int(math.Round(speed * float64(f.maxValue)))
				_ = c.send("OutputCmd", map[string]any{
					"DeviceIndex":  d.index,
					"FeatureIndex": f.featureIndex,
					"Command":      map[string]any{"Vibrate": map[string]any{"Value": value}},
				})
			}
		}
	}
}

func (c *buttplugClient) stopAll() {
	c.mu.Lock()
	version := c.version
	c.mu.Unlock()
	for _, d := range c.snapshotDevices() {
		if version >= 4 {
			_ = c.send("StopCmd", map[string]any{"DeviceIndex": d.index})
		} else {
			_ = c.send("StopDeviceCmd", map[string]any{"DeviceIndex": d.index})
		}
	}
}

// ----------------------------------------------------------------- main ----

// session runs one connection until it drops or sig fires. Returns true if
// the program should exit (signal received), false to reconnect.
func session(ws *wsConn, sig chan os.Signal) bool {
	client := newButtplugClient(ws)
	if err := client.connect(); err != nil {
		fmt.Println("Handshake failed:", err)
		return false
	}

	dropped := make(chan struct{})
	msgs := make(chan []byte, 64)
	go func() {
		defer close(dropped)
		for {
			data, err := ws.readMessage()
			if err != nil {
				return
			}
			select {
			case msgs <- data:
			case <-dropped:
				return
			}
		}
	}()
	go func() {
		for data := range msgs {
			client.handle(data)
		}
	}()

	fmt.Println("Waiting for devices... (turn your toy on and connect it in Intiface Central)")
	fmt.Println("Ready. Keep the WoW window in the foreground and take some damage!")
	fmt.Println("Press Ctrl+C to stop.")

	var lastFlash time.Time
	var flashGen uint64
	var wasRed bool
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	// Rescan every 30s until a vibration device shows up.
	rescan := time.NewTicker(30 * time.Second)
	defer rescan.Stop()

	for {
		select {
		case <-sig:
			client.stopAll()
			fmt.Println("Stopped.")
			return true
		case <-dropped:
			fmt.Println("Connection to the buttplug server lost. Reconnecting...")
			client.stopAll()
			return false
		case <-rescan.C:
			if !client.hasVibrationDevice() {
				_ = client.send("StartScanning", nil)
			}
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

func main() {
	fmt.Println("WowOfButtplug - Butthurt bridge for WoW: Forever")
	fmt.Println("Waiting for the Intiface Central server on ws://" + serverURL + serverPath + " ...")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	for {
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
			select {
			case <-sig:
				fmt.Println("Stopped.")
				return
			case <-time.After(2 * time.Second):
			}
		}
		if err != nil {
			fmt.Println("Could not reach the buttplug server:", err)
			fmt.Println("Start Intiface Central, click 'Start Server', then run this program again.")
			return
		}

		if session(ws, sig) {
			ws.conn.Close()
			return
		}
		ws.conn.Close()
		time.Sleep(2 * time.Second)
	}
}
