package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"
	"fmt"

	"github.com/gorilla/websocket"
	"celebration/ledcontrol"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws"

// Wire format for Phase 1
type WSMessage struct {
	Type      string `json:"type"`       // e.g., "deal_won", "account_created", "ping"
	Effect    string `json:"effect"`     // e.g., "blink", "rainbow", "wipe"
	ColorHex  string `json:"color"`      // e.g., "#FF0000" or "FF0000"
	Cycles    int    `json:"cycles"`     // optional repeats for some effects
	AccountID string `json:"accountId"`  // optional future use
}

// minimal fmt.Sscanf wrapper to keep imports tidy
// (you can also just import "fmt" at top and call fmt.Sscanf directly)
func fmtSscanf(str, format string, a ...interface{}) (int, error) {
	return fmt.Sscanf(str, format, a...)
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

	// Optional: keepalive so idle connections don’t die
	c.SetReadLimit(1 << 20)
	c.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.SetPongHandler(func(string) error {
		_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Background pinger
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

		// Back-compat: support plain "celebrate"
		if string(raw) == "celebrate" {
			log.Println("🎉 Celebration Triggered! (legacy)")
			ledcontrol.RunEffect("blink", 0x00FF00, 3) // green blink default
			continue
		}

		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Ignoring non-JSON message: %s\n", string(raw))
			continue
		}

		msg.Type = strings.TrimSpace(strings.ToLower(msg.Type))
		msg.Effect = strings.TrimSpace(strings.ToLower(msg.Effect))
		color := parseHexColor(msg.ColorHex)
		if msg.Cycles <= 0 {
			msg.Cycles = 3
		}

		switch msg.Type {
		case "deal_won", "account_created", "celebrate":
			if msg.Effect == "" {
				msg.Effect = "blink"
			}
			log.Printf("🎉 %s → effect=%s color=%06X cycles=%d\n", msg.Type, msg.Effect, color, msg.Cycles)
			ledcontrol.RunEffect(msg.Effect, color, msg.Cycles)

		case "ping":
			// no-op for now
			log.Println("← ping")
		default:
			// Unknown type: still do something fun
			log.Printf("Unknown type=%q, running default celebrate.\n", msg.Type)
			ledcontrol.RunEffect("blink", 0x0000FF, 2) // blue blink default
		}
	}
}

func parseHexColor(s string) uint32 {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) != 6 {
		return 0xFF7F00 // fallback: orange
	}
	var r, g, b uint32
	// parse as RRGGBB
	if _, err := sscanf(s, "%02x%02x%02x", &r, &g, &b); err == nil {
		return (r << 16) | (g << 8) | b
	}
	return 0xFF7F00
}

// tiny sscanf helper without pulling extra deps
func sscanf(str, format string, a ...interface{}) (int, error) {
	return fmtSscanf(str, format, a...)
}
