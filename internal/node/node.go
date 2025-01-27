package node

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// Message struct to represent messages exchanged between nodes
type Message struct {
	SenderID string
	Content  string
}

// Node struct representing a single network node
type Node struct {
	ID           string
	X, Y         float64             // Node position
	inbox        chan Message        // Message reception channel
	routingTable map[string]struct{} // Keeps track of discovered nodes
	mutex        sync.Mutex          // Ensures safe concurrent access
}

// Ensure Node implements INode interface at compile time
var _ INode = (*Node)(nil)

// NewNode creates a new node with a unique UUID
func NewNode(x, y float64) INode {
	return &Node{
		ID:           uuid.New().String(),
		X:            x,
		Y:            y,
		inbox:        make(chan Message, 100), // Buffer to prevent blocking
		routingTable: make(map[string]struct{}),
	}
}

// GetId returns the node's unique ID
func (n *Node) GetId() string {
	return n.ID
}

// GetPosition returns the node's position coordinates
func (n *Node) GetPosition() (float64, float64) {
	return n.X, n.Y
}

// StartListening starts a goroutine to listen for incoming messages
func (n *Node) StartListening() {
	// go func - anonymous function launched as a goroutine
	go func() {
		// Runs indefinitely, waiting for messages
		for msg := range n.inbox {
			go n.ProcessMessage(msg) // Spawn goroutine per message
		}
	}()
}

// ProcessMessage handles incoming messages
func (n *Node) ProcessMessage(msg Message) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if _, exists := n.routingTable[msg.SenderID]; !exists {
		n.routingTable[msg.SenderID] = struct{}{}
		fmt.Printf("Node %s discovered new node: %s\n", n.ID, msg.SenderID)
	}
}

// ReceiveMessage sends a message to the node's inbox
func (n *Node) ReceiveMessage(msg Message) error {
	n.inbox <- msg // Send message to inbox
	return nil
}

// SendMessage sends a message to another node
func (n *Node) SendMessage(to INode, message string) error {
	msg := Message{SenderID: n.ID, Content: message}
	return to.ReceiveMessage(msg)
}

// Broadcast sends a message to all nodes in the network
func (n *Node) Broadcast(nodes []INode) {
	msg := Message{SenderID: n.ID, Content: "DISCOVERY"}
	for _, node := range nodes {
		if node.GetId() != n.ID {
			go node.ReceiveMessage(msg)
		}
	}
}

// GetRoutingTable returns a copy of the routing table
func (n *Node) GetRoutingTable() map[string]struct{} {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	// Create a copy of the routing table to avoid race conditions
	tableCopy := make(map[string]struct{})
	for k, v := range n.routingTable {
		tableCopy[k] = v
	}
	return tableCopy
}
