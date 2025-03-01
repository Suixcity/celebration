package main

import (
	"log"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws" // Updated for WebSocket connection

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
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket connection lost, reconnecting...")
			break
		}

		if string(message) == "celebrate" {
			log.Println("🎉 Celebration Triggered!")
			triggerLED() // Call Go-based LED control function
		}
	}
}

func triggerLED() {
	log.Println("Running LED Go script...")

	// Execute led_control.go
	cmd := exec.Command("go", "run", "celebration/Client/led_control.go")
	err := cmd.Run()
	if err != nil {
		log.Println("Error executing LED Go script:", err)
	}
}

func main() {
	log.Println("Starting WebSocket Client...")
	connectToWebSocket()
}
