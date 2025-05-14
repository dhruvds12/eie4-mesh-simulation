package node

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/packet"
	"mesh-simulation/internal/routing"
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

	muUsers        sync.RWMutex
	connectedUsers map[uint32]bool

	eventBus *eventBus.EventBus
}

// NewNode creates a new Node with a given ID.
func NewNode(lat, long float64, bus *eventBus.EventBus) mesh.INode {
	nodeID := rand.Int31()
	var nodeIdInt = uint32(nodeID)
	log.Printf("[sim] Created new node ID: %d, x: %f, y: %f", nodeID, lat, long)
	return &nodeImpl{
		id:             nodeIdInt,
		coordinates:    mesh.CreateCoordinates(lat, long),
		messages:       make(chan []byte, 20),
		quit:           make(chan struct{}),
		seenBroadcasts: make(map[string]bool),
		neighbors:      make(map[uint32]bool),
		router:         routing.NewAODVRouter(nodeIdInt, bus),
		eventBus:       bus,
		connectedUsers: make(map[uint32]bool),
	}
}

// GetID returns the node's ID.
func (n *nodeImpl) GetID() uint32 {
	return n.id
}

// Run is the main goroutine for the node, processing incoming messages.
func (n *nodeImpl) Run(net mesh.INetwork) {
	log.Printf("Node %d: started.\n", n.id)
	defer log.Printf("Node %d: stopped.\n", n.id)

	if aodv, ok := n.router.(*routing.AODVRouter); ok {
		aodv.StartPendingTxChecker(net, n)
		aodv.StartBroadcastTicker(net, n)

		aodv.SendDiffBroadcastInfo(net, n)
	}

	for {
		select {
		case msg := <-n.messages:
			n.HandleMessage(net, msg)
		case <-n.quit:
			if aodv, ok := n.router.(*routing.AODVRouter); ok {
				aodv.StopPendingTxChecker()
				aodv.StopBroadcastTicker()
				aodv.StopTxWorker()
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
	n.router.SendData(net, n, destID, payload)
}

// send user message
func (n *nodeImpl) SendUserMessage(net mesh.INetwork, userID, destUserID uint32, payload string) {
	n.router.SendUserMessage(net, n, userID, destUserID, payload)
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
	// n.router.SendBroadcastInfo(net, n)
	n.router.SendDiffBroadcastInfo(net, n)
}

// HandleMessage processes an incoming message.
func (n *nodeImpl) HandleMessage(net mesh.INetwork, receivedPacket []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(receivedPacket); err != nil {
		log.Printf("Node %d: failed to deserialize BaseHeader: %v", n.id, err)
		return
	}
	switch bh.PacketType {
	// case message.MsgHelloAck: // TODO why is this separate to data ack -> should all fall under ack
	// 	log.Printf("[sim] Node %s: received HELLO_ACK from %s, payload=%q\n",
	// 		n.id, msg.GetFrom(), msg.GetPayload())
	// 	n.muNeighbors.Lock()
	// 	n.neighbors[bh.SrcNodeID] = true
	// 	n.router.AddDirectNeighbor(n.id, bh.SrcNodeID)
	// 	n.muNeighbors.Unlock()
	case packet.PKT_DATA, packet.PKT_RREP, packet.PKT_RREQ, packet.PKT_RERR, packet.PKT_ACK, packet.PKT_BROADCAST_INFO, packet.PKT_BROADCAST, packet.PKT_UREP, packet.PKT_UREQ, packet.PKT_UERR, packet.PKT_USER_MSG:
		n.router.HandleMessage(net, n, receivedPacket)
	default:
		log.Printf("Node %d: unknown message type from %d\n", n.id, bh.SrcNodeID)
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
	fmt.Printf("  ID:          %d\n", n.id)
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
		fmt.Printf("    - %d\n", neighborID)
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

func (n *nodeImpl) IsVirtual() bool {
	return true
}

func (n *nodeImpl) AddConnectedUser(userID uint32) {
	n.muUsers.Lock()
	defer n.muUsers.Unlock()
	n.connectedUsers[userID] = true
}

// RemoveConnectedUser unregisters a BLE‑disconnected user.
func (n *nodeImpl) RemoveConnectedUser(userID uint32) {
	n.muUsers.Lock()
	defer n.muUsers.Unlock()
	delete(n.connectedUsers, userID)
}

// GetConnectedUsers returns a snapshot of all currently connected userIDs.
func (n *nodeImpl) GetConnectedUsers() []uint32 {
	n.muUsers.RLock()
	defer n.muUsers.RUnlock()
	list := make([]uint32, 0, len(n.connectedUsers))
	for uid := range n.connectedUsers {
		list = append(list, uid)
	}
	return list
}

// HasConnectedUser lets you test membership
func (n *nodeImpl) HasConnectedUser(userID uint32) bool {
	n.muUsers.RLock()
	defer n.muUsers.RUnlock()
	return n.connectedUsers[userID]
}

func (n *nodeImpl) SetRouterConstants(CCAWindow, CCASample, InitialBackoff, MaxBackoff time.Duration, BackoffScheme string, BEUnit time.Duration, BEMaxExp int) bool {

	if aodv, ok := n.GetRouter().(*routing.AODVRouter); ok {
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

func (n *nodeImpl) GetRandomKnownNode() (uint32, bool) {
	aodv, ok := n.router.(*routing.AODVRouter)
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
		if id != n.id { // never pick myself
			keys = append(keys, id)
		}
	}
	if len(keys) == 0 {
		return 0, false
	}
	return keys[rand.Intn(len(keys))], true
}


func (n *nodeImpl) SetRoutingParams(th, rreqLim, ureqLim int) bool {
	if r, ok := n.router.(*routing.AODVRouter); ok {
		r.SetRoutingParams(th, rreqLim, ureqLim)
		return true
	}
	return false
}
