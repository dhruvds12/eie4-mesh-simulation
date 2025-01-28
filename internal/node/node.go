package node

import (
	"fmt"
	"sync"
	"time"

	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

// nodeImpl is a concrete implementation of INode.
type nodeImpl struct {
	id             uuid.UUID
	messages       chan message.IMessage
	quit           chan struct{}
	seenBroadcasts map[string]bool

	muNeighbors sync.RWMutex
	neighbors   map[uuid.UUID]bool
}

// NewNode creates a new Node with a given ID.
func NewNode() mesh.INode {
	return &nodeImpl{
		id:             uuid.New(),
		messages:       make(chan message.IMessage, 20),
		quit:           make(chan struct{}),
		seenBroadcasts: make(map[string]bool),
		neighbors:      make(map[uuid.UUID]bool),
	}
}

// GetID returns the node's ID.
func (n *nodeImpl) GetID() uuid.UUID {
	return n.id
}

// Run is the main goroutine for the node, processing incoming messages.
func (n *nodeImpl) Run(net mesh.INetwork) {
	fmt.Printf("Node %s: started.\n", n.id)
	defer fmt.Printf("Node %s: stopped.\n", n.id)

	for {
		select {
		case msg := <-n.messages:
			n.HandleMessage(net, msg)
		case <-n.quit:
			return
		}
	}
}

// SendData sends a unicast DATA message to a specific destination.
func (n *nodeImpl) SendData(net mesh.INetwork, destID uuid.UUID, payload string) {
	m := &message.Message{
		Type:    message.MsgData,
		From:    n.id,
		To:      destID,
		ID:      "", // Not a broadcast
		Payload: payload,
	}
	net.UnicastMessage(m, n)
}

// BroadcastHello sends a HELLO broadcast announcing the node’s presence.
func (n *nodeImpl) BroadcastHello(net mesh.INetwork) {
	// Create a unique broadcast ID to deduplicate
	broadcastID := fmt.Sprintf("hello-%s-%d", n.id, time.Now().UnixNano())

	to, err := uuid.Parse(message.BroadcastID)

	if err != nil {
		panic(fmt.Sprintf("Node %s: failed to parse broadcast ID: %v\n", n.id, err))
	}

	m := &message.Message{
		Type:    message.MsgHello,
		From:    n.id,
		To:      to,
		ID:      broadcastID,
		Payload: fmt.Sprintf("Hello from %s", n.id),
	}
	net.BroadcastMessage(m, n)
}

// HandleMessage processes an incoming message.
func (n *nodeImpl) HandleMessage(net mesh.INetwork, msg message.IMessage) {
	switch msg.GetType() {
	case message.MsgHello:
		n.handleHello(net, msg)
	case message.MsgHelloAck:
		fmt.Printf("Node %s: received HELLO_ACK from %s, payload=%q\n",
			n.id, msg.GetFrom(), msg.GetPayload())
	case message.MsgData:
		fmt.Printf("Node %s: received DATA from %s, payload=%q\n",
			n.id, msg.GetFrom(), msg.GetPayload())
	default:
		fmt.Printf("Node %s: unknown message type from %s\n", n.id, msg.GetFrom())
	}
}

// handleHello processes a HELLO broadcast message.
func (n *nodeImpl) handleHello(net mesh.INetwork, msg message.IMessage) {
	// Check for duplicate broadcasts
	if n.seenBroadcasts[msg.GetID()] {
		return
	}
	n.seenBroadcasts[msg.GetID()] = true

	fmt.Printf("Node %s: received HELLO from %s, payload=%q\n",
		n.id, msg.GetFrom(), msg.GetPayload())

	// We won't re-broadcast to avoid infinite loops in a fully connected scenario.
	// Instead, send a unicast HELLO_ACK back.
	ack := &message.Message{
		Type:    message.MsgHelloAck,
		From:    n.id,
		To:      msg.GetFrom(),
		ID:      "", // Not a broadcast
		Payload: fmt.Sprintf("HelloAck from %s", n.id),
	}
	net.UnicastMessage(ack, n)
}

func (n *nodeImpl) GetMessageChan() chan message.IMessage {
	return n.messages
}

func (n *nodeImpl) GetQuitChan() chan struct{} {
	return n.quit
}
