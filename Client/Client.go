package main

import (
	"log"
	"time"

	"celebration/ledcontrol" // Change this to match your actual project folder

	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws" // Update this URL to your own server

func connectToWebSocket() {
	retryDelay := 5 * time.Second

	for {
		c, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
		if err != nil {
			log.Printf("Failed to connect: %v. Retrying in %v...\n", err, retryDelay)
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff (up to a limit)
			if retryDelay > 60*time.Second {
				retryDelay = 60 * time.Second
			}
			continue
		}

		log.Println("Connected to WebSocket server")
		retryDelay = 5 * time.Second // Reset delay on success
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
	defer func() {
		log.Println("Closing WebSocket connection...")
		c.Close()
	}()

}

func main() {
	log.Println("Starting WebSocket Client...")
	connectToWebSocket()
}
