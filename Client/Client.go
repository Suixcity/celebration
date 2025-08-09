package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"celebration/ledcontrol"

	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws"
var conn *websocket.Conn // Store connection globally

func connectToWebSocket() {
	retryDelay := 5 * time.Second

	for {
		c, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
		if err != nil {
			log.Printf("Failed to connect: %v. Retrying in %v...\n", err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2
			if retryDelay > 60*time.Second {
				retryDelay = 60 * time.Second
			}
			continue
		}

		log.Println("Connected to WebSocket server")
		retryDelay = 5 * time.Second // Reset delay on success
		conn = c                     // Store connection globally
		handleMessages(c)
	}
}

func handleMessages(c *websocket.Conn) {
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
