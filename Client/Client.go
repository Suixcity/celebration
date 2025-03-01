package main

import (
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/gorilla/websocket"
)

var serverURL = "wss://webhook-listener-2i7r.onrender.com/ws" // WebSocket URL

func installDependencies() {
	log.Println("Checking for missing dependencies...")

	// Ensure Go modules are initialized
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		log.Println("Initializing Go module...")
		exec.Command("go", "mod", "init", "celebration").Run()
	}

	// Install required Go packages
	dependencies := []string{
		"github.com/gorilla/websocket",
		"github.com/rpi-ws281x/rpi-ws281x-go",
	}
	for _, dep := range dependencies {
		log.Println("Installing:", dep)
		exec.Command("go", "get", dep).Run()
	}

	log.Println("Dependencies installed.")
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
	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket connection lost, reconnecting...")
			break
		}

		if string(message) == "celebrate" {
			log.Println("🎉 Celebration Triggered!")
			triggerLED()
		}
	}
}

func triggerLED() {
	log.Println("Running LED Go script...")

	cmd := exec.Command("go", "run", "led_control.go")
	cmd.Dir = "./"
	err := cmd.Run()
	if err != nil {
		log.Println("Error executing LED Go script:", err)
	}
}

func main() {
	log.Println("Starting WebSocket Client...")
	installDependencies() // Ensures everything is installed
	connectToWebSocket()
}
