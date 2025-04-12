package node

import (
	"fmt"
	"log"
	"sync"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"
	"mesh-simulation/internal/routing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// physicalNode is a concrete implementation of INode for physical nodes.
type physicalNode struct {
	id             uint32
	coordinates    mesh.Coordinates
	messages       chan []byte
	quit           chan struct{}
	commandTopic   string
	statusTopic    string
	router         routing.IRouter
	mqttManager    mqtt.Client
	muNeighbors    sync.RWMutex
	neighbors      map[uint32]bool
	seenBroadcasts map[string]bool
	eventBus       *eventBus.EventBus
}

// NewPhysicalNode creates a new physical node using parameters received via MQTT registration.
func NewPhysicalNode(nodeID uint32, commandTopic, statusTopic string, lat, long float64, bus *eventBus.EventBus, mqttClient mqtt.Client) mesh.INode {
	// Try to parse the incoming nodeID; if invalid, generate a new one
	// TODO: hardware id's and simulation ids are incompatible currently need to update the simulation
	log.Printf("[sim] Created new physical node ID: %d, x: %f, y: %f", nodeID, lat, long) //TODO: is this correct
	return &physicalNode{
		id:             nodeID,
		coordinates:    mesh.CreateCoordinates(lat, long),
		messages:       make(chan []byte, 20),
		quit:           make(chan struct{}),
		commandTopic:   commandTopic,
		statusTopic:    statusTopic,
		neighbors:      make(map[uint32]bool),
		seenBroadcasts: make(map[string]bool),
		eventBus:       bus,
		router:         routing.NewAODVRouter(nodeID, bus),
		mqttManager:    mqttClient,
	}
}

// GetID returns the node's ID.
func (p *physicalNode) GetID() uint32 {
	return p.id
}

// Run starts the main processing loop for the physical node.
func (p *physicalNode) Run(net mesh.INetwork) {
	log.Printf("Physical Node %s: started.\n", p.id)
	defer log.Printf("Physical Node %s: stopped.\n", p.id)

	// Optionally, subscribe to its command topic using the central MQTT manager.
	// For example, you might register a dedicated callback that handles commands:
	token := p.mqttManager.Subscribe(p.commandTopic, 0, p.handleMQTTCommand)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Physical Node %s: error subscribing to command topic: %v", p.id, token.Error())
	}

	// Process internal messages (which might include translated MQTT events) as well as other events.
	for {
		select {
		case msg := <-p.messages:
			p.HandleMessage(net, msg)
		case <-p.quit:
			return
		}
	}
}

// SendData sends data to a specified destination using the node’s router.
func (p *physicalNode) SendData(net mesh.INetwork, destID uint32, payload string) {
	p.router.SendDataCSMA(net, p, destID, payload)

	// also need to send a message to the physical node to send a messge
}

// BroadcastHello sends a HELLO broadcast from this physical node.
func (p *physicalNode) BroadcastHello(net mesh.INetwork) {
	p.router.BroadcastHello(net, p)
	// need to send a message to a physical node
}

// HandleMessage processes an incoming message.
func (p *physicalNode) HandleMessage(net mesh.INetwork, msg []byte) {
	// add physical-specific handling.
	switch msg.GetType() {
	case message.MsgHello:
		p.router.HandleMessage(net, p, msg)
	case message.MsgHelloAck:
		log.Printf("[sim] Node %s: received HELLO_ACK from %s, payload=%q\n",
			p.id, msg.GetFrom(), msg.GetPayload())
		p.muNeighbors.Lock()
		p.neighbors[msg.GetFrom()] = true
		p.router.AddDirectNeighbor(p.id, msg.GetFrom())
		p.muNeighbors.Unlock()
	case message.MsgData, message.MsgRREP, message.MsgRREQ, message.MsgRERR, message.DataAck:
		p.router.HandleMessage(net, p, msg)
	default:
		log.Printf("Physical Node %s: unknown message type from %s\n", p.id, msg.GetFrom())
	}
}

// GetMessageChan returns the message channel.
func (p *physicalNode) GetMessageChan() chan []byte {
	return p.messages
}

// GetQuitChan returns the quit channel.
func (p *physicalNode) GetQuitChan() chan struct{} {
	return p.quit
}

// GetPosition returns the node's current coordinates.
func (p *physicalNode) GetPosition() mesh.Coordinates {
	return p.coordinates
}

// SetPosition sets the node's coordinates.
func (p *physicalNode) SetPosition(coord mesh.Coordinates) {
	p.coordinates = coord
}

// PrintNodeDetails prints details specific to this physical node.
func (p *physicalNode) PrintNodeDetails() {
	fmt.Println("====================================")
	fmt.Println("Physical Node Details:")
	fmt.Printf("  ID:          %s\n", p.id)
	fmt.Printf("  Coordinates: (Lat: %.2f, Long: %.2f)\n", p.coordinates.Lat, p.coordinates.Long)
	fmt.Printf("  Command Topic: %s\n", p.commandTopic)
	fmt.Printf("  Status Topic:  %s\n", p.statusTopic)
	fmt.Printf("  Messages:    %d messages in queue\n", len(p.messages))
	fmt.Printf("  Quit Signal: %v\n", p.quit != nil)
	fmt.Println("  Seen Broadcasts:")
	for broadcastID := range p.seenBroadcasts {
		fmt.Printf("    - %s\n", broadcastID)
	}
	fmt.Println("  Neighbors:")
	p.muNeighbors.RLock()
	for neighborID := range p.neighbors {
		fmt.Printf("    - %s\n", neighborID)
	}
	p.muNeighbors.RUnlock()
	fmt.Println("  Router:")
	fmt.Printf("    - %T\n", p.router)
	// print out routing table
	fmt.Println("  Routing Table:")
	r := p.router.(*routing.AODVRouter)
	r.PrintRoutingTable()
	fmt.Println("====================================")
}

// handleMQTTCommand processes MQTT messages sent to this node's command topic.
func (p *physicalNode) handleMQTTCommand(client mqtt.Client, msg mqtt.Message) {
	// In this callback, convert the MQTT message to an internal message format,
	// then send it on the physical node’s message channel for processing.
	log.Printf("Physical Node %s received command: %s\n", p.id, msg.Payload())
	/*
		Create a message type that can handle this
		- send message on simulation
		- add to routing table
		- remove from routing table
		- move?
	*/
}
