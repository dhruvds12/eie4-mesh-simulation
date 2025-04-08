package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"mesh-simulation/internal/commands"
	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Define a WebSocket upgrader.
var upgrader = websocket.Upgrader{
	// Allow any origin for simplicity. Adjust for production use.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsHandler upgrades the connection to WebSocket and pushes events from the EventBus.
func wsHandler(eb *eventBus.EventBus, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe to the event bus.
	eventCh := eb.Subscribe()

	// Loop indefinitely, writing events to the WebSocket.
	for event := range eventCh {
		if err := conn.WriteJSON(event); err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}

// commandHandler is a simple REST endpoint to accept commands from the front end.
func commandHandler(eb *eventBus.EventBus, w http.ResponseWriter, r *http.Request) {
	// For example, the front end might POST a command like {"command": "join", "node_id": "node123"}.
	var cmd map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert node_id to a uuid.UUID.
	nodeID, err := uuid.Parse(fmt.Sprintf("%v", cmd["node_id"]))
	if err != nil {
		log.Printf("Invalid node_id: %v", cmd["node_id"])
		http.Error(w, "Invalid node_id", http.StatusBadRequest)
		return
	}

	// Create an event. Here we define a new event type "COMMAND_RECEIVED" on the fly.
	event := eventBus.Event{
		Type:      eventBus.EventType("COMMAND_RECEIVED"),
		NodeID:    nodeID,
		Payload:   fmt.Sprintf("Command: %v", cmd["command"]),
		Timestamp: time.Now(),
	}

	eb.Publish(event)

	w.Write([]byte("Command received"))
}

// StartServer starts the HTTP server with endpoints for WebSocket and commands.
func StartServer(eb *eventBus.EventBus, net mesh.INetwork) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(eb, w, r)
	})
	http.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		commandHandler(eb, w, r)
	})

	// Setup command endpoints.
	http.HandleFunc("/nodeAPI/create", commands.CreateNodeHandler(net, eb))
	http.HandleFunc("/nodeAPI/remove", commands.RemoveNodeHandler(net, eb))
	http.HandleFunc("/nodeAPI/sendMessage", commands.SendMessageHandler(net, eb))
	http.HandleFunc("/nodeAPI/move", commands.MoveNodeHandler(net, eb))

	// Start the HTTP server (e.g. on port 8080).
	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
