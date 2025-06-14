package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/routing"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/vmihailenco/msgpack/v5"
)

// Constants for actions—should match those used in hardware.
const (
	ACTION_MESSAGE          uint8 = 0x01
	ACTION_UPDATE_ROUTE     uint8 = 0x02
	ACTION_INVALIDATE_ROUTE uint8 = 0x03
	ADD_USER                uint8 = 0x04
	REMOVE_USER             uint8 = 0x05
	ADD_USER_ROUTE          uint8 = 0x06
	REMOVE_USER_ROUTE       uint8 = 0x07
)

// NodeCommand represents a command sent from the hardware node.
type NodeCommand struct {
	Action      uint8  `json:"action" msgpack:"action"`
	Destination uint32 `json:"destination,omitempty" msgpack:"destination,omitempty"`
	NextHop     uint32 `json:"next_hop,omitempty" msgpack:"next_hop,omitempty"`
	HopCount    uint8  `json:"hop_count,omitempty" msgpack:"hop_count,omitempty"`
	PacketID    uint32 `json:"packet_id,omitempty" msgpack:"packet_id,omitempty"`
	Payload     []byte `json:"payload,omitempty" msgpack:"payload,omitempty"`
	PayloadLen  uint32 `json:"payload_len,omitempty" msgpack:"payload_len,omitempty"`
	UserId      uint32 `json:"user_id,omitempty" msgpack:"user_id,omitempty"`
}

// physicalNode is a concrete implementation of INode for physical nodes.
type physicalNode struct {
	id             uint32
	coordinates    mesh.Coordinates
	messages       chan []byte
	quit           chan struct{}
	commandTopic   string
	processTopic   string
	sendTopic      string
	router         routing.IRouter
	mqttManager    mqtt.Client
	muNeighbors    sync.RWMutex
	neighbors      map[uint32]bool
	seenBroadcasts map[string]bool
	eventBus       *eventBus.EventBus
	net            mesh.INetwork
	muUsers        sync.RWMutex
	connectedUsers map[uint32]bool
}

// NewPhysicalNode creates a new physical node using parameters received via MQTT registration.
func NewPhysicalNode(nodeID uint32, commandTopic, processTopic, sendTopic string, lat, long float64, bus *eventBus.EventBus, mqttClient mqtt.Client) mesh.INode {
	// Try to parse the incoming nodeID; if invalid, generate a new one
	// TODO: hardware id's and simulation ids are incompatible currently need to update the simulation
	log.Printf("[sim] Created new physical node ID: %d, x: %f, y: %f", nodeID, lat, long) //TODO: is this correct
	return &physicalNode{
		id:             nodeID,
		coordinates:    mesh.CreateCoordinates(lat, long),
		messages:       make(chan []byte, 20),
		quit:           make(chan struct{}),
		commandTopic:   commandTopic,
		processTopic:   processTopic,
		sendTopic:      sendTopic,
		neighbors:      make(map[uint32]bool),
		seenBroadcasts: make(map[string]bool),
		eventBus:       bus,
		router:         routing.NewAODVRouter(nodeID, bus),
		mqttManager:    mqttClient,
		connectedUsers: make(map[uint32]bool),
	}
}

// GetID returns the node's ID.
func (p *physicalNode) GetID() uint32 {
	return p.id
}

// Run starts the main processing loop for the physical node.
func (p *physicalNode) Run(net mesh.INetwork) {
	p.net = net
	log.Printf("Physical Node %d: started.\n", p.id)
	defer log.Printf("Physical Node %d: stopped.\n", p.id)

	// Optionally, subscribe to its command topic using the central MQTT manager.
	// For example, you might register a dedicated callback that handles commands:
	token := p.mqttManager.Subscribe(p.commandTopic, 0, p.handleMQTTCommand)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Physical Node %d: error subscribing to command topic: %v", p.id, token.Error())
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
// TODO added flags variable not sent over mqtt!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!! -------------------------------------------------
func (p *physicalNode) SendData(net mesh.INetwork, destID uint32, payload string, flags uint8) {
	// p.router.SendDataCSMA(net, p, destID, payload)

	// also need to send a message to the physical node to send a messge using lora
	cmd := struct {
		Destination uint32 `json:"destination"`
		Message     string `json:"message"`
	}{
		Destination: destID,
		Message:     payload,
	}

	buf, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("Physical Node %d: failed to marshal send_message JSON: %v", p.id, err)
		return
	}
	token := p.mqttManager.Publish(p.sendTopic, 0, false, buf)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Physical Node %d: error publishing send_message: %v", p.id, token.Error())
	} else {
		log.Printf("Physical Node %d: published send_message JSON to %s: %s",
			p.id, p.sendTopic, buf)
	}

}

// send user message
func (p *physicalNode) SendUserMessage(net mesh.INetwork, userID, destUserID uint32, payload string, flags uint8) {
	p.router.SendUserMessage(net, p, userID, destUserID, payload, flags)

	// Publish to mqtt and tell the node to send a message to the user.

}

// SendBroadcastInfo sends a HELLO broadcast from this physical node.
func (p *physicalNode) SendBroadcastInfo(net mesh.INetwork) {
	// p.router.SendBroadcastInfo(net, p)
	// need to send a message to a physical node
	// p.router.SendDiffBroadcastInfo(net, p)

	// Do nothing ===> physical node will handle this behaviour itself
}

// HandleMessage processes an incoming message.
func (p *physicalNode) HandleMessage(net mesh.INetwork, receivedPacket []byte) {
	// add physical-specific handling.
	// fmt.Printf("Payload (hex): %x\n", receivedPacket)
	// fmt.Printf("Payload: %v\n", receivedPacket)
	// payloadString := string(receivedPacket)
	// fmt.Printf("Payload: %v\n", payloadString)

	log.Printf("Physical Node: %d, sent message over mqtt topic %s", p.id, p.processTopic)

	// send the message over mqtt to the physical node

	err := p.mqttManager.Publish(p.processTopic, 0, false, receivedPacket)
	if err != nil {
		log.Printf("Physical Node %d: error publishing: %v", p.id, err)
	}
	// var bh packet.BaseHeader
	// if err := bh.DeserialiseBaseHeader(receivedPacket); err != nil {
	// 	log.Printf("Node %d: failed to deserialize BaseHeader: %v", p.id, err)
	// 	return
	// }
	// switch bh.PacketType {
	// // case message.MsgHelloAck:
	// // 	log.Printf("[sim] Node %s: received HELLO_ACK from %s, payload=%q\n",
	// // 		p.id, msg.GetFrom(), msg.GetPayload())
	// // 	p.muNeighbors.Lock()
	// // 	p.neighbors[msg.GetFrom()] = true
	// // 	p.router.AddDirectNeighbor(p.id, msg.GetFrom())
	// // 	p.muNeighbors.Unlock()
	// case packet.PKT_DATA, packet.PKT_RREP, packet.PKT_RREQ, packet.PKT_RERR, packet.PKT_ACK, packet.PKT_BROADCAST_INFO:
	// 	p.router.HandleMessage(net, p, receivedPacket)
	// default:
	// 	log.Printf("Physical Node %d: unknown message type from %d\n", p.id, bh.SrcNodeID)
	// }
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

func (p *physicalNode) IsVirtual() bool {
	return false
}

// PrintNodeDetails prints details specific to this physical node.
func (p *physicalNode) PrintNodeDetails() {
	fmt.Println("====================================")
	fmt.Println("Physical Node Details:")
	fmt.Printf("  ID:          %d\n", p.id)
	fmt.Printf("  Coordinates: (Lat: %.2f, Long: %.2f)\n", p.coordinates.Lat, p.coordinates.Long)
	fmt.Printf("  Command Topic: %s\n", p.commandTopic)
	fmt.Printf("  Process Topic:  %s\n", p.processTopic)
	fmt.Printf("  Send Topic:  %s\n", p.sendTopic)
	fmt.Printf("  Messages:    %d messages in queue\n", len(p.messages))
	fmt.Printf("  Quit Signal: %v\n", p.quit != nil)
	fmt.Println("  Seen Broadcasts:")
	for broadcastID := range p.seenBroadcasts {
		fmt.Printf("    - %s\n", broadcastID)
	}
	fmt.Println("  Neighbors:")
	p.muNeighbors.RLock()
	for neighborID := range p.neighbors {
		fmt.Printf("    - %d\n", neighborID)
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
	rawPayload := msg.Payload()
	// Print the raw bytes in hexadecimal
	log.Printf("Physical Node %d: Raw MQTT message bytes: % x", p.id, rawPayload)

	// Also, print the raw message as a string (if printable)
	log.Printf("Physical Node %d: Raw MQTT message string: %s", p.id, rawPayload)
	payload := bytes.TrimRight(msg.Payload(), "\x00")

	var cmd NodeCommand

	// First, try decoding as JSON.
	err := json.Unmarshal(payload, &cmd)
	if err != nil {
		log.Printf("Physical Node %d: error decoding MQTT command (JSON failed): %v", p.id, err)
		// If JSON decoding fails, try MessagePack.
		err = msgpack.Unmarshal(payload, &cmd)
		if err != nil {
			log.Printf("Physical Node %d: error decoding MQTT command (JSON & MessagePack failed): %v", p.id, err)
			return
		}
	}

	log.Printf("Physical Node %d received command: %+v", p.id, cmd)

	log.Printf("Physical Node %d received command: %+v", p.id, cmd)

	// Now switch on the action.
	switch cmd.Action {
	case ACTION_UPDATE_ROUTE:
		// Handle route update.
		// For example, publish an event or update the routing table.
		p.router.AddRouteEntry(cmd.Destination, cmd.NextHop, int(cmd.HopCount))
		log.Printf("Physical Node %d: Update Route - destination: %d, nextHop: %d, hopCount: %d",
			p.id, cmd.Destination, cmd.NextHop, cmd.HopCount)

	case ACTION_INVALIDATE_ROUTE:
		// Handle route invalidation.
		p.router.RemoveRouteEntry(cmd.Destination)
		log.Printf("Physical Node %d: Invalidate Route - destination: %d", p.id, cmd.Destination)

	case ACTION_MESSAGE:
		// Handle a packet (message) command.
		// In this case, we assume that the packet command was sent via MessagePack,
		// so cmd.Payload contains the raw binary packet.
		log.Printf("Physical Node %d: Received message command - packet_id: %d, payload length: %d",
			p.id, cmd.PacketID, cmd.PayloadLen)
		// Dispatch the message
		p.router.BroadcastMessageCSMA(p.net, p, cmd.Payload, cmd.PacketID)
	case ADD_USER:
		// Add user to connected users table - need to first disect the message
		p.AddConnectedUser(cmd.UserId)

		// This needs to be sent to the front end
		p.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventCreateUser,
			NodeID: p.id,
			UserID: cmd.UserId,
		})

	case REMOVE_USER:
		// Remove user from connected users table
		p.RemoveConnectedUser(cmd.UserId)
		p.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventDeleteUser,
			NodeID: p.id,
			UserID: cmd.UserId,
		})

	case ADD_USER_ROUTE:
		// Update GUT

	case REMOVE_USER_ROUTE:
		// Remove from GUT
	default:
		log.Printf("Physical Node %d: unknown command action: %d", p.id, cmd.Action)
	}
}

func (p *physicalNode) AddConnectedUser(userID uint32) {
	p.muUsers.Lock()
	defer p.muUsers.Unlock()
	p.connectedUsers[userID] = true
}

// RemoveConnectedUser unregisters a BLE‑disconnected user.
func (p *physicalNode) RemoveConnectedUser(userID uint32) {
	p.muUsers.Lock()
	defer p.muUsers.Unlock()
	delete(p.connectedUsers, userID)
}

// GetConnectedUsers returns a snapshot of all currently connected userIDs.
func (p *physicalNode) GetConnectedUsers() []uint32 {
	p.muUsers.RLock()
	defer p.muUsers.RUnlock()
	list := make([]uint32, 0, len(p.connectedUsers))
	for uid := range p.connectedUsers {
		list = append(list, uid)
	}
	return list
}

// HasConnectedUser lets you test membership
func (p *physicalNode) HasConnectedUser(userID uint32) bool {
	p.muUsers.RLock()
	defer p.muUsers.RUnlock()
	return p.connectedUsers[userID]
}

func (p *physicalNode) GetRouter() routing.IRouter {
	return p.router
}

func (p *physicalNode) SetRouter(r routing.IRouter) {
	p.router = r
}

func (p *physicalNode) SetRouterConstants(CCAWindow, CCASample, InitialBackoff, MaxBackoff time.Duration, BackoffScheme string, BEUnit time.Duration, BEMaxExp int) bool {

	if aodv, ok := p.GetRouter().(*routing.AODVRouter); ok {
		aodv.CcaWindow = CCAWindow
		aodv.CcaSample = CCASample
		aodv.InitialBackoff = InitialBackoff
		aodv.MaxBackoff = MaxBackoff
		aodv.BackoffScheme = BackoffScheme
		aodv.BeUnit = BEUnit
		aodv.BeMaxExp = BEMaxExp
		return ok
	}

	return false

}

func (p *physicalNode) GetRandomKnownNode() (uint32, bool) {
	aodv, ok := p.router.(*routing.AODVRouter)
	if !ok {
		return 0, false
	}
	aodv.RouteMu.RLock()
	defer aodv.RouteMu.RUnlock()

	if len(aodv.RouteTable) == 0 {
		return 0, false
	}
	keys := make([]uint32, 0, len(aodv.RouteTable))
	for id := range aodv.RouteTable {
		if id != p.id { // never pick myself
			keys = append(keys, id)
		}
	}
	if len(keys) == 0 {
		return 0, false
	}
	return keys[rand.Intn(len(keys))], true
}

func (p *physicalNode) SetRoutingParams(th, rreqLim, ureqLim int) bool {
	if r, ok := p.router.(*routing.AODVRouter); ok {
		r.SetRoutingParams(th, rreqLim, ureqLim)
		return true
	}
	return false
}

func (p *physicalNode) GetRandomKnownUser() (userID uint32, ok bool) {
    gut := p.router.GUTSnapshot()
    if len(gut) == 0 {
        return 0, false
    }
    keys := make([]uint32, 0, len(gut))
    for u := range gut {
        keys = append(keys, u)
    }
    // pick one at random
    return keys[rand.Intn(len(keys))], true
}