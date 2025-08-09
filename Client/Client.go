package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"celebration/ledcontrol"
	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws"

// Message from server
type WSMessage struct {
	Type      string `json:"type"`      // e.g., "deal_won"
	Effect    string `json:"effect"`    // optional override from server
	ColorHex  string `json:"color"`     // optional override "#RRGGBB"
	Cycles    int    `json:"cycles"`    // optional override
	AccountID string `json:"accountId"` // optional future
}

// Device preferences
type EffectPref struct {
	Effect string `json:"effect"`
	Color  string `json:"color"`
	Cycles int    `json:"cycles"`
}
type DeviceConfig struct {
	Events map[string]EffectPref `json:"events"`
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

func resolvePrefs(msg WSMessage) (effect string, color uint32, cycles int) {
	// 1) base from device defaults by event type
	if p, ok := deviceCfg.Events[msg.Type]; ok {
		effect = p.Effect
		color = parseHexColor(p.Color)
		cycles = p.Cycles
	}
	// 2) server overrides, if provided
	if msg.Effect != "" {
		effect = msg.Effect
	}
	if msg.ColorHex != "" {
		color = parseHexColor(msg.ColorHex)
	}
	if msg.Cycles > 0 {
		cycles = msg.Cycles
	}
	// 3) fallbacks
	if effect == "" {
		effect = "celebrate_legacy" // calls BlinkLEDs() to preserve legacy behavior
	}
	if color == 0 {
		color = 0x00FF00
	}
	if cycles <= 0 {
		cycles = 1
	}
	return
}

func connectToWebSocket() {
	for {
		c, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
		if err != nil {
			log.Println("Failed to connect to server, retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Connected to WebSocket server")
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
			ledcontrol.RunEffectByName("celebrate_legacy", 0x00FF00, 1)
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
		ledcontrol.RunEffectByName(effect, color, cycles)
	}
}

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
	connectToWebSocket()
}
