package main

import (
	"fmt"
	"log"
	"net/http"

	"celebration/Client/ledcontrol"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var clients = make(map[*websocket.Conn]bool)

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}
	defer conn.Close()

	clients[conn] = true
	log.Println("Client connected")

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Client disconnected")
			delete(clients, conn)
			break
		}

		command := string(message)
		log.Println("Received command:", command)

		// Handle LED commands clearleds doesnt work for some reason???
		switch command {
		case "celebrate":
			ledcontrol.BlinkLEDs()
		case "off":
			ledcontrol.ClearLEDs()
		default:
			log.Println("Unknown command:", command)
		}
	}
}

func serveWebApp() {
	fs := http.FileServer(http.Dir("./WebServer/web"))
	http.Handle("/", fs)
	http.HandleFunc("/ws", handleWebSocket)

	port := "8080"
	fmt.Println("Web UI running on http://localhost:" + port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
}

func main() {
	serveWebApp()
}
