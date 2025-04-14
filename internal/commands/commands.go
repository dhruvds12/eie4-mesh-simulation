package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/node"
)

// CreateNodePayload defines the expected JSON payload for node creation.
type CreateNodePayload struct {
	Lat  float64 `json:"lat"`
	Long float64 `json:"long"`
}

// CreateNodeHandler creates a new node and adds it to the network.
func CreateNodeHandler(net mesh.INetwork, bus *eventBus.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload CreateNodePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Create a new node using the provided coordinates and event bus.
		newNode := node.NewNode(payload.Lat, payload.Long, bus)
		// Add the node to the network.
		net.Join(newNode)

		// Publish an event to inform subscribers that a new node has joined.
		// TODO: Should move this can't really check that a node has been created.
		bus.Publish(eventBus.Event{
			Type:      eventBus.EventNodeJoined,
			NodeID:    newNode.GetID(),
			Payload:   fmt.Sprintf("Node %d created and joined the network", newNode.GetID()),
			Timestamp: time.Now(),
			X:         newNode.GetPosition().Lat,
			Y:         newNode.GetPosition().Long,
			Virtual:   true,
		})

		w.Write([]byte("Node created and joined the network"))
	}
}

// RemoveNodePayload defines the expected JSON payload for removing a node.
type RemoveNodePayload struct {
	NodeID uint32 `json:"node_id"`
}

// RemoveNodeHandler removes a node from the network.
func RemoveNodeHandler(net mesh.INetwork, bus *eventBus.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload RemoveNodePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Parse the node ID string to a uuid.UUID.
		nodeID := payload.NodeID

		// Remove the node from the network.
		net.Leave(nodeID)

		// Publish an event to inform subscribers that a node has left.
		bus.Publish(eventBus.Event{
			Type:      eventBus.EventNodeLeft,
			NodeID:    nodeID,
			Payload:   fmt.Sprintf("Node %d removed from the network", nodeID),
			Timestamp: time.Now(),
			Virtual:   true,
		})

		w.Write([]byte("Node removed from the network"))
	}
}

type SendMessagePayload struct {
	SenderNodeID      uint32 `json:"node_id"`
	DestinationNodeID uint32 `json:"dest_node_id"`
	Message           string `json:"message"`
}

// Send a message to a node
func SendMessageHandler(net mesh.INetwork, bus *eventBus.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload SendMessagePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		nodeID := payload.SenderNodeID

		destNodeID := payload.DestinationNodeID

		// need to get node and send the message
		senderNode, err := net.GetNode(nodeID)
		if err != nil {
			http.Error(w, "Sender node_id not found", http.StatusBadRequest)
			return
		}

		senderNode.SendData(net, destNodeID, payload.Message)

		w.Write([]byte("Sending Data ..."))
	}
}

type MoveNodePayload struct {
	NodeID uint32  `json:"node_id"`
	Lat    float64 `json:"lat"`
	Long   float64 `json:"long"`
}

// Move a node
func MoveNodeHandler(net mesh.INetwork, bus *eventBus.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload MoveNodePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		nodeID := payload.NodeID

		node, err := net.GetNode(nodeID)
		if err != nil {
			http.Error(w, "node_id not found", http.StatusBadRequest)
			return
		}
		w.Write([]byte("Moving Node ..."))

		position := mesh.CreateCoordinates(payload.Lat, payload.Long)

		node.SetPosition(position)

		bus.Publish(eventBus.Event{
			Type:      eventBus.EventMovedNode,
			NodeID:    nodeID,
			Payload:   fmt.Sprintf("Moved Node %d ", nodeID),
			Timestamp: time.Now(),
			X:         node.GetPosition().Lat,
			Y:         node.GetPosition().Long,
		})

	}
}
