package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"celebration/ledcontrol"

	"github.com/gorilla/websocket"
)

type WSMessage struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params,omitempty"`
}

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws"
var conn *websocket.Conn // Store connection globally

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

/*func handleMessages(c *websocket.Conn) {
	defer c.Close()
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket connection lost, reconnecting...")
			break
		}

		msg := string(message)
		switch msg {
		case "account_created":
			log.Println("📩 Account created → Celebration animation")
			ledcontrol.BlinkLEDs()

		case "deal_created":
			log.Println("📩 Deal created → Shoot animation")
			ledcontrol.ShootBounceLEDs(
				0xFF0000,            // RGB cycle
				8,                   // tail
				12*time.Millisecond, // frameDelay
				1,                   // Bounces
			)
		case "deal_won":
			log.Println("📩 Deal won → Stacked Shoot")
			ledcontrol.DealWonStackedShootHalfTrigger(
				[]uint32{0xFF0000, 0x0000FF, 0x00FF00}, // palette
				8,                                      // tail
				12*time.Millisecond,                    // frameDelay
				2,                                      // maxActive (2 shots at most)
				3,                                      // blinkCount
				180*time.Millisecond,                   // blinkPeriod
			)

		default:
			log.Printf("📩 Unhandled message: %q\n", msg)
		}
	}
}*/

func handleMessages(c *websocket.Conn) {
	defer c.Close()
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket connection lost, reconnecting...")
			break
		}

		// Try JSON first
		var msg WSMessage
		if err := json.Unmarshal(raw, &msg); err == nil && msg.Type != "" {
			switch msg.Type {
			// Phase 0 routing (minimal): wire known types; expand later
			case "account_created":
				log.Println("📩 account_created → Celebration")
				ledcontrol.BlinkLEDs()

			case "deal_created":
				log.Println("📩 deal_created → (TODO) shoot/bounce")
				// TODO in next phases: ledcontrol.ShootBounceLEDs(...)

			case "deal_won":
				log.Println("📩 deal_won → (TODO) stacked half-trigger")
				// TODO in next phases: ledcontrol.DealWonStackedShootHalfTrigger(...)

			default:
				log.Printf("📩 Unhandled type %q; ignoring\n", msg.Type)
			}
			continue
		}

		// Legacy fallback (pre‑JSON)
		switch string(raw) {
		case "celebrate":
			log.Println("📩 legacy 'celebrate' → Celebration")
			ledcontrol.BlinkLEDs()
		default:
			log.Printf("📩 Legacy/unrecognized message: %q\n", string(raw))
		}
	}
}

// Handle graceful shutdown
func handleShutdown() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c // Wait for signal
	log.Println("Shutting down...")

	if conn != nil {
		log.Println("Closing WebSocket connection...")
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}

	log.Println("Client closed gracefully.")
	os.Exit(0)
}

func main() {
	log.Println("Starting WebSocket Client...")

	err := ledcontrol.InitLEDs()
	if err != nil {
		log.Fatalf("Failed to initialize LEDs: %v", err)
	}

	ledcontrol.RunBreathingEffect()

	// Run WebSocket connection in a separate goroutine
	go connectToWebSocket()

	// Handle graceful shutdown
	handleShutdown()
}
