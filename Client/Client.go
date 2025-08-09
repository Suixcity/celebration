package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"celebration/ledcontrol"

	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws"

// ---------- Incoming WS message ----------
type WSMessage struct {
	Type      string `json:"type"`      // e.g., "deal_won"
	Effect    string `json:"effect"`    // optional override
	ColorHex  string `json:"color"`     // optional override "#RRGGBB"
	Cycles    int    `json:"cycles"`    // optional override
	AccountID string `json:"accountId"` // optional
}

// ---------- Device config (config.json) ----------
type EffectPref struct {
	Effect string `json:"effect"`
	Color  string `json:"color"`
	Cycles int    `json:"cycles"`
}
type IdlePref struct {
	Effect string `json:"effect"` // "breath", "solid", "rainbow", etc. (must be supported by RunEffectByName)
	Color  string `json:"color"`
	Cycles int    `json:"cycles"` // 0 or <1 = loop forever for non-breath idles
}
type DeviceConfig struct {
	Idle   IdlePref              `json:"idle"`
	Events map[string]EffectPref `json:"events"`
}
type ClientIdent struct {
	DeviceID     string `json:"deviceId"`
	DeviceSecret string `json:"deviceSecret"`
}

var deviceCfg = DeviceConfig{Events: map[string]EffectPref{}}

func cfgPath() string { return filepath.Join(".", "config.json") }

func loadConfig() {
	data, err := os.ReadFile(cfgPath())
	if err != nil {
		log.Printf("config.json not found (using no defaults): %v", err)
		return
	}
	if err := json.Unmarshal(data, &deviceCfg); err != nil {
		log.Printf("config.json invalid (ignored): %v", err)
	}
}

func loadIdent() (ClientIdent, error) {
	var id ClientIdent
	b, err := os.ReadFile(filepath.Join(".", "client.json"))
	if err != nil {
		return id, fmt.Errorf("read client.json: %w", err)
	}
	if err := json.Unmarshal(b, &id); err != nil {
		return id, fmt.Errorf("parse client.json: %w", err)
	}
	if strings.TrimSpace(id.DeviceID) == "" || strings.TrimSpace(id.DeviceSecret) == "" {
		return id, fmt.Errorf("client.json missing deviceId or deviceSecret")
	}
	return id, nil
}

func sign(deviceID, secret, ts string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(deviceID))
	mac.Write([]byte(":"))
	mac.Write([]byte(ts))
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------- Idle manager (runs whatever you put in config.json) ----------
var (
	idleMu      sync.Mutex
	idleStopCh  chan struct{}
	idleRunning bool
)

// startIdle starts whichever idle effect is in config.json.
// - If "breath": uses your RunBreathingEffect()/StopBreathingEffect() (continuous).
// - Else: loops RunEffectByName(effect, color, cyclesOrDefault) in a goroutine.
func startIdle() {
	idleMu.Lock()
	defer idleMu.Unlock()
	if idleRunning {
		return
	}
	effect := strings.ToLower(strings.TrimSpace(deviceCfg.Idle.Effect))
	if effect == "" {
		return
	}

	switch effect {
	case "breath", "runbreathingeffect":
		ledcontrol.RunBreathingEffect()
		idleRunning = true
		// breathing stops via stopIdle() -> StopBreathingEffect()
	default:
		idleStopCh = make(chan struct{})
		idleRunning = true
		color := parseHexColor(deviceCfg.Idle.Color)

		// For idle: if cycles < 1, we’ll loop forever.
		idleCycles := deviceCfg.Idle.Cycles
		if idleCycles < 1 {
			idleCycles = 1 // RunEffectByName once per loop iteration
		}

		go func(name string, col uint32, cyc int) {
			log.Printf("Idle loop start: %s color=%06X cycles=%d", name, col, cyc)
			defer log.Printf("Idle loop exit: %s", name)

			for {
				select {
				case <-idleStopCh:
					return
				default:
				}
				// Run the effect once; for "solid", your win.go should set and not clear.
				ledcontrol.RunEffectByName(name, col, cyc)

				// Small pause between loops for animated idles
				select {
				case <-idleStopCh:
					return
				case <-time.After(100 * time.Millisecond):
				}
			}
		}(effect, color, idleCycles)
	}
}

func stopIdle() {
	idleMu.Lock()
	defer idleMu.Unlock()
	if !idleRunning {
		return
	}
	effect := strings.ToLower(strings.TrimSpace(deviceCfg.Idle.Effect))
	if effect == "breath" || effect == "runbreathingeffect" {
		ledcontrol.StopBreathingEffect()
	} else if idleStopCh != nil {
		close(idleStopCh)
		idleStopCh = nil
	}
	idleRunning = false
}

// ---------- Preferences resolution ----------
func resolvePrefs(msg WSMessage) (effect string, color uint32, cycles int) {
	// 1) start from device defaults
	if p, ok := deviceCfg.Events[msg.Type]; ok {
		effect = strings.ToLower(strings.TrimSpace(p.Effect))
		color = parseHexColor(p.Color)
		cycles = p.Cycles
	}
	// 2) server overrides
	if msg.Effect != "" {
		effect = strings.ToLower(strings.TrimSpace(msg.Effect))
	}
	if msg.ColorHex != "" {
		color = parseHexColor(msg.ColorHex)
	}
	if msg.Cycles > 0 {
		cycles = msg.Cycles
	}
	// 3) fallbacks
	if effect == "" {
		effect = "celebrate_legacy" // calls your BlinkLEDs()
	}
	if color == 0 {
		color = 0x00FF00
	}
	if cycles <= 0 {
		cycles = 1
	}
	return
}

// ---------- WebSocket client ----------
func connectToWebSocket() {
	ident, err := loadIdent()
	if err != nil {
		log.Fatalf("identity error: %v", err)
	}

	for {
		ts := fmt.Sprintf("%d", time.Now().Unix())

		hdr := map[string][]string{
			"X-Device-ID": {ident.DeviceID},
			"X-Auth-Ts":   {ts},
			"X-Auth-Sig":  {sign(ident.DeviceID, ident.DeviceSecret, ts)},
		}

		c, _, err := websocket.DefaultDialer.Dial(serverURL, hdr)
		if err != nil {
			log.Println("WS connect failed (auth or net), retrying in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Connected to WebSocket server as", ident.DeviceID)
		handleMessages(c)
	}
}

func handleMessages(c *websocket.Conn) {
	defer c.Close()

	// keepalive
	c.SetReadLimit(1 << 20)
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.SetPongHandler(func(string) error {
		return c.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			_ = c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
		}
	}()

	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket connection lost, reconnecting...")
			return
		}

		// legacy: plain "celebrate"
		if string(raw) == "celebrate" {
			log.Println("🎉 Legacy celebrate (string) received.")
			stopIdle()
			ledcontrol.RunEffectByName("celebrate_legacy", 0x00FF00, 1)
			startIdle()
			continue
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Ignoring non-JSON message: %q\n", string(raw))
			continue
		}
		msg.Type = strings.TrimSpace(strings.ToLower(msg.Type))
		msg.Effect = strings.TrimSpace(strings.ToLower(msg.Effect))

		effect, color, cycles := resolvePrefs(msg)
		log.Printf("Event=%s → effect=%s color=%06X cycles=%d\n", msg.Type, effect, color, cycles)

		stopIdle()
		ledcontrol.RunEffectByName(effect, color, cycles)
		startIdle()
	}
}

// ---------- utils ----------
func parseHexColor(s string) uint32 {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) != 6 {
		return 0
	}
	var r, g, b uint32
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err == nil {
		return (r << 16) | (g << 8) | b
	}
	return 0
}

func main() {
	log.Println("Starting WebSocket Client...")
	loadConfig()
	startIdle() // start whatever idle is configured
	connectToWebSocket()
}
