package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var clients = make(map[*websocket.Conn]bool) // Connected clients
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("Received Webhook: %v\n", data)

	if event, ok := data["event"].(string); ok {
		switch event {
		case "account_created":
			log.Println("👤 Account Created - Sending animation")
			broadcastMessage("account_created") // account animation

		case "deal_created":
			log.Println("💼 Deal Created - Sending animation")
			broadcastMessage("deal_created") // deal animation

		default:
			http.Error(w, "Unknown event", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success","message":"LED triggered"}`))
		return
	}

	http.Error(w, "Invalid event", http.StatusBadRequest)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket Upgrade Error:", err)
		return
	}

	clients[conn] = true
	log.Println("Client connected")

	defer func() {
		log.Println("Client disconnected")
		delete(clients, conn) // Cleanup
		conn.Close()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Println("WebSocket Connection Closed")
			break
		}
	}
}

func broadcastMessage(message string) {
	for client := range clients {
		if err := client.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			log.Println("Error sending message to client:", err)
			client.Close()
			delete(clients, client)
		}
	}
}

func main() {
	http.HandleFunc("/", handleWebhook)
	http.HandleFunc("/ws", handleWebSocket)

	port := "10000" // Matches Render setup
	fmt.Println("Server listening on port", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
}
