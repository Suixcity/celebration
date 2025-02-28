package main

import (
	"fmt"
	"log"
	"time"
	"github.com/gorilla/websocket"
	"os/exec"
)

var serverURL = "https://celebration-je6z.onrender.com"

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
			exec.Command("python3", "led_control.py").Run() // Call Python script for LED animation
		}
	}
}

func main() {
	log.Println("Starting WebSocket Client...")
	connectToWebSocket()
}