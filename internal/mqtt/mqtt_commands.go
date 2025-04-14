package mqtt

import (
	"encoding/json"
	"fmt"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/node"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ProcessMqttNodeMessage handles messages coming from the "simulation/register" topic.
func ProcessMqttNodeMessage(net mesh.INetwork, bus *eventBus.EventBus) func(mqtt.Client, mqtt.Message) {
	return func(client mqtt.Client, msg mqtt.Message) {
		var payload MqttNodePayload
		if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
			fmt.Printf("Error parsing MQTT payload: %v\n", err)
			return
		}

		switch payload.Event {
		case "register":
			// Create a new physical node based on the registration payload.
			newNode := node.NewPhysicalNode(payload.NodeID, payload.CommandTopic, payload.StatusTopic, payload.Lat, payload.Long, bus, client)
			// Add the new node to the network.
			net.Join(newNode)

			// Publish an event on the event bus.
			bus.Publish(eventBus.Event{
				Type:      eventBus.EventNodeJoined,
				NodeID:    newNode.GetID(),
				Payload:   fmt.Sprintf("Physical Node %d registered and joined the network", newNode.GetID()),
				Timestamp: time.Now(),
				X:         newNode.GetPosition().Lat,
				Y:         newNode.GetPosition().Long,
				Virtual:   false,
			})
			fmt.Printf("Node %d registered successfully\n", newNode.GetID())

		case "remove":
			// Remove the node from the network.
			nodeID := payload.NodeID

			net.Leave(nodeID)

			// Publish an event on the event bus.
			bus.Publish(eventBus.Event{
				Type:      eventBus.EventNodeLeft,
				NodeID:    nodeID,
				Payload:   fmt.Sprintf("Physical Node %d removed from the network", payload.NodeID),
				Timestamp: time.Now(),
				Virtual:   false,
			})
			fmt.Printf("Node %d removed successfully\n", payload.NodeID)

		default:
			fmt.Printf("Unknown event type: %s\n", payload.Event)
		}
	}
}
