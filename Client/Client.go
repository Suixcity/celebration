package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"celebration/ledcontrol"

	"github.com/gorilla/websocket"
)

var (
	// Change these to wherever Server.go is running
	apiBase = "https://webhook-listener-2i7r.onrender.com"
	wsURL   = "wss://webhook-listener-2i7r.onrender.com/ws"
)

// ---------- types ----------
type WSMessage struct {
	Type     string `json:"type"`
	Effect   string `json:"effect"`
	ColorHex string `json:"color"`
	Cycles   int    `json:"cycles"`
}

type EffectPref struct {
	Effect string `json:"effect"`
	Color  string `json:"color"`
	Cycles int    `json:"cycles"`
}
type IdlePref struct {
	Effect string `json:"effect"`
	Color  string `json:"color"`
	Cycles int    `json:"cycles"`
}
type DevicePrefs struct {
	Idle   IdlePref              `json:"idle"`
	Events map[string]EffectPref `json:"events"`
}
type ClientIdent struct {
	DeviceID     string `json:"deviceId"`
	DeviceSecret string `json:"deviceSecret"`
}

type effectJob struct {
	effect string
	color  uint32
	cycles int
}

var (
	devicePrefs = DevicePrefs{Events: map[string]EffectPref{}}
	jobs        = make(chan effectJob, 32) // serialize effects
)

// ---------- identity & signing ----------
func loadIdent() (ClientIdent, error) {
	var id ClientIdent
	b, err := os.ReadFile(filepath.Join(".", "client.json"))
	if err != nil {
		return id, err
	}
	if err := json.Unmarshal(b, &id); err != nil {
		return id, err
	}
	if strings.TrimSpace(id.DeviceID) == "" || strings.TrimSpace(id.DeviceSecret) == "" {
		return id, fmt.Errorf("client.json missing deviceId or deviceSecret")
	}
	return id, nil
}
func sign(deviceID, secret, ts string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(deviceID))
	m.Write([]byte(":"))
	m.Write([]byte(ts))
	return hex.EncodeToString(m.Sum(nil))
}

// ---------- small utils ----------
func parseHexColor(s string) uint32 {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0
	}
	var r, g, b uint32
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0
	}
	return (r << 16) | (g << 8) | b
}
func must[T any](v T, _ error) T { return v }

// ---------- keep local config.json’s idle color in sync ----------
func writeIdleColorIntoLocalConfig(hexColor string) {
	type idleCfg struct {
		Color string `json:"color"`
	}
	type conf struct {
		LedPin     int     `json:"ledPin"`
		LedCount   int     `json:"ledCount"`
		Brightness int     `json:"brightness"`
		Idle       idleCfg `json:"idle"`
	}
	var c conf
	_ = json.Unmarshal(must(os.ReadFile("config.json")), &c)
	if c.Idle.Color == hexColor {
		return
	}
	c.Idle.Color = hexColor
	_ = os.WriteFile("config.json", must(json.MarshalIndent(c, "", "  ")), 0644)
	log.Printf("Updated local config.json idle.color to %s", hexColor)
}

// ---------- prefs fetch & apply ----------
func fetchPrefs(deviceID string) {
	url := fmt.Sprintf("%s/devices/%s/prefs", apiBase, deviceID)
	res, err := http.Get(url)
	if err != nil {
		log.Printf("fetch prefs: %v", err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		log.Printf("fetch prefs status %d: %s", res.StatusCode, string(b))
		return
	}
	var p DevicePrefs
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		log.Printf("prefs decode: %v", err)
		return
	}
	devicePrefs = p

	// Sync idle color for breathing effect (win.go reads config.json)
	if p.Idle.Color != "" {
		writeIdleColorIntoLocalConfig(p.Idle.Color)
	}
	// Restart idle to pick up new effect/color
	ledcontrol.StopBreathingEffect()
	if strings.ToLower(strings.TrimSpace(p.Idle.Effect)) == "breath" ||
		strings.ToLower(strings.TrimSpace(p.Idle.Effect)) == "runbreathingeffect" {
		ledcontrol.RunBreathingEffect()
	}
	log.Printf("Applied prefs: idle=%s %s, %d events", p.Idle.Effect, p.Idle.Color, len(p.Events))
}

// ---------- event resolution ----------
func resolvePrefs(msg WSMessage) (effect string, color uint32, cycles int) {
	// start from device prefs by event type
	if p, ok := devicePrefs.Events[strings.ToLower(strings.TrimSpace(msg.Type))]; ok {
		effect = strings.ToLower(strings.TrimSpace(p.Effect))
		color = parseHexColor(p.Color)
		cycles = p.Cycles
	}
	// server overrides
	if msg.Effect != "" {
		effect = strings.ToLower(strings.TrimSpace(msg.Effect))
	}
	if msg.ColorHex != "" {
		color = parseHexColor(msg.ColorHex)
	}
	if msg.Cycles > 0 {
		cycles = msg.Cycles
	}

	// fallbacks
	if effect == "" {
		effect = "celebrate_legacy"
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
	// set your deployed URLs
	const wsURL = "wss://webhook-listener-2i7r.onrender.com/ws"

	ident, err := loadIdent() // reads client.json {deviceId, deviceSecret}
	if err != nil {
		log.Fatalf("identity error: %v", err)
	}

	for {
		ts := fmt.Sprintf("%d", time.Now().Unix())
		hdr := http.Header{
			"X-Device-ID": []string{ident.DeviceID},
			"X-Auth-Ts":   []string{ts},
			"X-Auth-Sig":  []string{sign(ident.DeviceID, ident.DeviceSecret, ts)},
		}

		d := *websocket.DefaultDialer
		c, resp, err := d.Dial(wsURL, hdr)
		if err != nil {
			// Print server’s actual response to see why the handshake failed
			if resp != nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				log.Printf("WS connect failed (%s): HTTP %d %s body=%q", wsURL, resp.StatusCode, resp.Status, string(body))
			} else {
				log.Printf("WS connect failed: %v", err)
			}
			time.Sleep(5 * time.Second)
			continue
		}

		log.Println("Connected to WebSocket server as", ident.DeviceID)
		handleMessages(c, ident)
		// handleMessages returns on disconnect; loop will retry
	}
}

func handleMessages(c *websocket.Conn, ident ClientIdent) {
	defer c.Close()

	// keepalive
	c.SetReadLimit(1 << 20)
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.SetPongHandler(func(string) error { return c.SetReadDeadline(time.Now().Add(60 * time.Second)) })
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

		// config push
		if string(raw) == `{"type":"config_updated"}` || strings.Contains(string(raw), `"config_updated"`) {
			log.Println("Config update notice → refetching prefs")
			fetchPrefs(ident.DeviceID)
			continue
		}

		// JSON event?
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err == nil && (msg.Type != "" || msg.Effect != "") {
			effect, color, cycles := resolvePrefs(msg)
			log.Printf("Event=%s → effect=%s color=%06X cycles=%d", msg.Type, effect, color, cycles)
			jobs <- effectJob{effect, color, cycles}
			continue
		}

		// plain text event (e.g., "deal_won")
		text := strings.ToLower(strings.TrimSpace(string(raw)))
		if text != "" {
			effect, color, cycles := resolvePrefs(WSMessage{Type: text})
			log.Printf("Event=%s → effect=%s color=%06X cycles=%d", text, effect, color, cycles)
			jobs <- effectJob{effect, color, cycles}
		}
	}
}

// serialize effects; pause idle during effect, then resume
func startEffectWorker() {
	go func() {
		for job := range jobs {
			ledcontrol.StopBreathingEffect()
			ledcontrol.RunEffectByName(job.effect, job.color, job.cycles)
			// resume idle if configured as breath
			if strings.ToLower(strings.TrimSpace(devicePrefs.Idle.Effect)) == "breath" ||
				strings.ToLower(strings.TrimSpace(devicePrefs.Idle.Effect)) == "runbreathingeffect" {
				ledcontrol.RunBreathingEffect()
			}
		}
	}()
}

// ---------- main ----------
func main() {
	log.Println("Starting WebSocket Client...")

	// 1) fetch & apply prefs (sets config.json idle color; starts idle if breath)
	id, err := loadIdent()
	if err != nil {
		log.Fatalf("identity error: %v", err)
	}
	fetchPrefs(id.DeviceID)

	// 2) start effect worker
	startEffectWorker()

	// 3) connect WS (auth)
	connectToWebSocket()
}
