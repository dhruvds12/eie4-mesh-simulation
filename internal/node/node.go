package node

import (
	"fmt"
	"log"
	"math/rand"
	"sync"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"
	"mesh-simulation/internal/routing"
	"mesh-simulation/internal/packet"
)

// nodeImpl is a concrete implementation of INode.
type nodeImpl struct {
	id          uint32
	coordinates mesh.Coordinates
	messages    chan []byte
	quit        chan struct{}

	router routing.IRouter

	muNeighbors    sync.RWMutex
	neighbors      map[uint32]bool
	seenBroadcasts map[string]bool

	eventBus *eventBus.EventBus
}

// NewNode creates a new Node with a given ID.
func NewNode(lat, long float64, bus *eventBus.EventBus) mesh.INode {
	nodeID := rand.Int31()
	var nodeIdInt = uint32(nodeID)
	log.Printf("[sim] Created new node ID: %s, x: %f, y: %f", nodeID, lat, long)
	return &nodeImpl{
		id:             nodeIdInt,
		coordinates:    mesh.CreateCoordinates(lat, long),
		messages:       make(chan []byte, 20),
		quit:           make(chan struct{}),
		seenBroadcasts: make(map[string]bool),
		neighbors:      make(map[uint32]bool),
		router:         routing.NewAODVRouter(nodeIdInt, bus),
		eventBus:       bus,
	}
}

// GetID returns the node's ID.
func (n *nodeImpl) GetID() uint32 {
	return n.id
}

// Run is the main goroutine for the node, processing incoming messages.
func (n *nodeImpl) Run(net mesh.INetwork) {
	log.Printf("Node %s: started.\n", n.id)
	defer log.Printf("Node %s: stopped.\n", n.id)

	if aodv, ok := n.router.(*routing.AODVRouter); ok {
		aodv.StartPendingTxChecker(net, n)
	}

	for {
		select {
		case msg := <-n.messages:
			n.HandleMessage(net, msg)
		case <-n.quit:
			if aodv, ok := n.router.(*routing.AODVRouter); ok {
				aodv.StopPendingTxChecker()
			}
			return
		}
	}
}

// // SendData sends a unicast DATA message to a specific destination.
// func (n *nodeImpl) SendData(net mesh.INetwork, destID uint32, payload string) {
// 	m := &message.Message{
// 		Type:    message.MsgData,
// 		From:    n.id,
// 		To:      destID,
// 		ID:      "", // Not a broadcast
// 		Payload: payload,
// 	}
// 	net.UnicastMessage(m, n)
// }

// SendData is a convenience method calling into the router
func (n *nodeImpl) SendData(net mesh.INetwork, destID uint32, payload string) {
	n.router.SendDataCSMA(net, n, destID, payload)
}

// BroadcastHello sends a HELLO broadcast announcing the node’s presence.
func (n *nodeImpl) SendBroadcastInfo(net mesh.INetwork) {
	// // Create a unique broadcast ID to deduplicate
	// broadcastID := fmt.Sprintf("hello-%s-%d", n.id, time.Now().UnixNano())

	// to, err := uuid.Parse(message.BroadcastID)

	// if err != nil {
	// 	log.Fatalf("Node %s: failed to parse broadcast ID: %v", n.id, err)

	// }

	// m := &message.Message{
	// 	Type:    message.MsgHello,
	// 	From:    n.id,
	// 	To:      to,
	// 	ID:      broadcastID,
	// 	Payload: fmt.Sprintf("Hello from %s", n.id),
	// }
	// net.BroadcastMessage(m, n)
	n.router.SendBroadcastInfo(net, n)
}

// HandleMessage processes an incoming message.
func (n *nodeImpl) HandleMessage(net mesh.INetwork, receivedPacket []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(receivedPacket); err != nil {
		log.Printf("Node %s: failed to deserialize BaseHeader: %v", n.id, err)
		return
	}
	switch bh.PacketType {
	case message.MsgHello:
		n.router.HandleMessage(net, n, msg)
	case message.MsgHelloAck: // TODO why is this separate to data ack -> should all fall under ack
		log.Printf("[sim] Node %s: received HELLO_ACK from %s, payload=%q\n",
			n.id, msg.GetFrom(), msg.GetPayload())
		n.muNeighbors.Lock()
		n.neighbors[bh.SrcNodeID] = true
		n.router.AddDirectNeighbor(n.id, bh.SrcNodeID)
		n.muNeighbors.Unlock()
	case packet.PKT_DATA, packet.PKT_RREP, packet.PKT_RREQ, packet.PKT_RERR, message.DataAck:
		n.router.HandleMessage(net, n, msg)
	default:
		log.Printf("Node %s: unknown message type from %d\n", n.id, bh.SrcNodeID)
	}
}

// handleHello processes a HELLO broadcast message.
// func (n *nodeImpl) handleHello(net mesh.INetwork, msg message.IMessage) {
// 	// Check for duplicate broadcasts
// 	if n.seenBroadcasts[msg.GetID()] {
// 		return
// 	}
// 	n.seenBroadcasts[msg.GetID()] = true

// 	neighborID := msg.GetFrom()

// 	log.Printf("[sim] Node %s: received HELLO from %s, payload=%q\n",
// 		n.id, neighborID, msg.GetPayload())

// 	// Add the sender to the list of neighbors
// 	n.muNeighbors.Lock()
// 	n.neighbors[neighborID] = true
// 	n.router.AddDirectNeighbor(n.id, msg.GetFrom())
// 	n.muNeighbors.Unlock()

// 	// We won't re-broadcast to avoid infinite loops in a fully connected scenario.
// 	// Instead, send a unicast HELLO_ACK back.
// 	ack := &message.Message{
// 		Type:    message.MsgHelloAck,
// 		From:    n.id,
// 		To:      msg.GetFrom(),
// 		ID:      "", // Not a broadcast
// 		Payload: fmt.Sprintf("HelloAck from %s", n.id),
// 	}
// 	net.UnicastMessage(ack, n)
// }

func (n *nodeImpl) GetMessageChan() chan []byte {
	return n.messages
}

func (n *nodeImpl) GetQuitChan() chan struct{} {
	return n.quit
}

func (n *nodeImpl) GetPosition() mesh.Coordinates {
	return n.coordinates
}

func (n *nodeImpl) SetPosition(coord mesh.Coordinates) {
	n.coordinates = coord
}

// PrintNodeDetails prints the details of a node in a nicely formatted way
func (n *nodeImpl) PrintNodeDetails() {
	fmt.Println("====================================")
	fmt.Println("Node Details:")
	fmt.Printf("  ID:          %s\n", n.id)
	fmt.Printf("  Coordinates: (Lat: %.2f, Long: %.2f)\n", n.coordinates.Lat, n.coordinates.Long)
	fmt.Printf("  Messages:    %d messages in queue\n", len(n.messages))
	fmt.Printf("  Quit Signal: %v\n", n.quit != nil)
	fmt.Println("  Seen Broadcasts:")
	for broadcastID := range n.seenBroadcasts {
		fmt.Printf("    - %s\n", broadcastID)
	}
	fmt.Println("  Neighbors:")
	n.muNeighbors.RLock()
	for neighborID := range n.neighbors {
		fmt.Printf("    - %s\n", neighborID)
	}
	n.muNeighbors.RUnlock()
	fmt.Println("  Router:")
	fmt.Printf("    - %T\n", n.router)
	// print out routing table
	fmt.Println("  Routing Table:")
	r := n.router.(*routing.AODVRouter)
	r.PrintRoutingTable()

	fmt.Println("====================================")
}

func (n *nodeImpl) GetRouter() routing.IRouter {
	return n.router
}

func (n *nodeImpl) SetRouter(r routing.IRouter) {
	n.router = r
}
