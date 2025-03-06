package main

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
	"celebration/ledcontrol" // Change this to match your actual project folder
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws" // Update this URL to your own server

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
			log.Println(" Celebration Triggered!")
			ledcontrol.BlinkLEDs()
		}
	}
}

func main() {
	log.Println("Starting WebSocket Client...")
	connectToWebSocket()
}

