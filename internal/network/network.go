package network

import (
	"fmt"
	"sync"

	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

type networkImpl struct {
	mu    sync.RWMutex
	nodes map[uuid.UUID]mesh.INode

	joinRequests  chan mesh.INode
	leaveRequests chan uuid.UUID
}

// NewNetwork creates a new instance of the network.
func NewNetwork() mesh.INetwork {
	return &networkImpl{
		nodes:         make(map[uuid.UUID]mesh.INode),
		joinRequests:  make(chan mesh.INode),
		leaveRequests: make(chan uuid.UUID),
	}
}

// Run is the main goroutine for the network, handling joins/leaves.
func (net *networkImpl) Run() {
	for {
		select {
		case newNode := <-net.joinRequests:
			net.addNode(newNode)
		case nodeID := <-net.leaveRequests:
			net.removeNode(nodeID)
		}
	}
}

// Join adds a node to the network.
func (net *networkImpl) Join(n mesh.INode) {
	net.joinRequests <- n
}

// Leave removes a node from the network by ID.
func (net *networkImpl) Leave(nodeID uuid.UUID) {
	net.leaveRequests <- nodeID
}

// BroadcastMessage simulates a single broadcast transmission (fully connected).
func (net *networkImpl) BroadcastMessage(msg message.IMessage, sender mesh.INode) {
	net.mu.RLock()
	defer net.mu.RUnlock()

	for id, nd := range net.nodes {
		if id == sender.GetID() {
			continue
		}
		ndChan := net.getNodeChannel(nd)
		ndChan <- msg
	}
}

// UnicastMessage simulates a direct unicast from sender to msg.To().
func (net *networkImpl) UnicastMessage(msg message.IMessage, sender mesh.INode) {
	net.mu.RLock()
	defer net.mu.RUnlock()

	to := msg.GetTo()
	if receiver, ok := net.nodes[to]; ok {
		ndChan := net.getNodeChannel(receiver)
		ndChan <- msg
	} else {
		fmt.Printf("[Network] Node %q tried to send to unknown node %q.\n",
			sender.GetID(), to)
	}
}

// addNode inserts the node into the map, starts its goroutine, and triggers a broadcast HELLO.
func (net *networkImpl) addNode(n mesh.INode) {
	net.mu.Lock()
	net.nodes[n.GetID()] = n
	net.mu.Unlock()

	go n.Run(net)

	// Broadcast a HELLO so new node can discover neighbors.
	n.BroadcastHello(net)
}

// removeNode signals the node to stop and removes it from the map.
func (net *networkImpl) removeNode(nodeID uuid.UUID) {
	net.mu.Lock()
	if nd, ok := net.nodes[nodeID]; ok {
		// Close the node's quit channel if there's a direct handle
		// We don't have a direct reference to 'quit', so let's do a type check
		if ni, ok := nd.(interface{ QuitChan() chan struct{} }); ok {
			close(ni.QuitChan())
		}
		delete(net.nodes, nodeID)
	}
	net.mu.Unlock()
}

// getNodeChannel is a helper to get the 'messages' channel from the node.
// Because node.INode is an interface, we do a type assertion.
func (net *networkImpl) getNodeChannel(n mesh.INode) chan message.IMessage {
	type channelGetter interface {
		GetMessageChan() chan message.IMessage
	}
	if cg, ok := n.(channelGetter); ok {
		return cg.GetMessageChan()
	}
	panic("Node does not implement channelGetter interface")
}
