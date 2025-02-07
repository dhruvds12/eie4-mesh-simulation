package network

import (
	"log"
	"sync"
	"time"

	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

const (
	maxRange = 1400.0             // Maximum range for direct comms
	LoRaAirTime = 300 * time.Millisecond // how long a single broadcast is on-air
)

// Struct to hold the transmission details
type Transmission struct {
	Msg message.IMessage
	Sender mesh.INode
	StartTime time.Time
	EndTime time.Time
	Collided bool
	Recipients []mesh.INode
}

type networkImpl struct {
	mu    sync.RWMutex
	nodes map[uuid.UUID]mesh.INode

	joinRequests  chan mesh.INode
	leaveRequests chan uuid.UUID

	transmissions map[string]*Transmission
}


// NewNetwork creates a new instance of the network.
func NewNetwork() mesh.INetwork {
	return &networkImpl{
		nodes:         make(map[uuid.UUID]mesh.INode), // Potentially need to change to a pointer 
		joinRequests:  make(chan mesh.INode),
		leaveRequests: make(chan uuid.UUID),
		transmissions: make(map[string]*Transmission),
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
// func (net *networkImpl) BroadcastMessage(msg message.IMessage, sender mesh.INode) {
// 	net.mu.RLock()
// 	defer net.mu.RUnlock()

// 	for id, nd := range net.nodes {
// 		if id == sender.GetID() {
// 			continue
// 		}

// 		if net.IsInRange(sender, nd) {
// 			ndChan := net.getNodeChannel(nd)
// 			ndChan <- msg
// 			log.Printf("[Network] Node %q is IN range for broadcast.\n", id)
// 		} else {
// 			log.Printf("[Network] Node %q is OUT of range for broadcast.\n", id)
// 		}
// 	}
// }

// For collisions, we treat partial overlap as collision. 
func timesOverlap(s1, e1, s2, e2 time.Time) bool {
    return s1.Before(e2) && s2.Before(e1)
}

// If distance > 2*maxRange, no collision possible
func sendersCanCollide(senderA, senderB mesh.INode) bool {
    dist := senderA.GetPosition().DistanceTo(senderB.GetPosition())
    return dist <= (maxRange * 2.0)
}

// deliverIfNoCollision checks, for each recipient in tx.Recipients,
// whether that node sees any overlapping transmissions. If so, the message is dropped
// for that node; otherwise, the message is delivered.
func (net *networkImpl) deliverIfNoCollision(tx *Transmission, sender mesh.INode) {
	for _, nd := range tx.Recipients {
		// re-check if the node is still in range (might have moving nodes in the future)
		if !net.IsInRange(sender, nd) {
			log.Printf("[Network] Node %s is no longer in range for msg %s.\n", nd.GetID(), tx.Msg.GetID())
			continue
		}

		// Count overlapping transmissions that this recipient would hear.
		overlappingCount := 0
		for _, otherTx := range net.transmissions {
			if otherTx == tx {
				continue
			}
			// Check if the transmission intervals overlap.
			if timesOverlap(tx.StartTime, tx.EndTime, otherTx.StartTime, otherTx.EndTime) {
				// Check if this recipient is in range of the other transmitter.
				if net.IsInRange(otherTx.Sender, nd) {
					overlappingCount++
				}
			}
		}
		if overlappingCount > 0 {
			log.Printf("[Collision] at node %s Skipping delivery of message %s due to %d overlapping transmission(s).\n",
				nd.GetID(), tx.Msg.GetID(), overlappingCount)
		} else {
			nd.GetMessageChan() <- tx.Msg
			log.Printf("[Network] Delivered message %s to node %s.\n", tx.Msg.GetID(), nd.GetID())
		}
	}
}

// BroadcastMessage simulates a broadcast transmission.
// It pre-filters nodes that are in range of the sender and then schedules a callback
// to deliver the message using deliverIfNoCollision.
func (net *networkImpl) BroadcastMessage(msg message.IMessage, sender mesh.INode) {
	net.mu.Lock()

	start := time.Now()
	end := start.Add(LoRaAirTime)

	// Pre-filter nodes: build a list of nodes that are in range of the sender at transmission start.
	var recipients []mesh.INode
	for id, nd := range net.nodes {
		if id == sender.GetID() {
			continue
		}
		if net.IsInRange(sender, nd) {
			recipients = append(recipients, nd)
			log.Printf("[Network] Node %q pre-filtered as recipient for msg %s.\n", id, msg.GetID())
		}
	}

	// Create a transmission record.
	tx := &Transmission{
		Msg:        msg,
		Sender:     sender,
		StartTime:  start,
		EndTime:    end,
		Collided:   false,
		Recipients: recipients,
	}

	// Store the transmission.
	net.transmissions[msg.GetID()] = tx

	// Check against ongoing transmissions to mark collisions.
	for _, ongoing := range net.transmissions {
		if ongoing == tx {
			continue
		}
		if timesOverlap(start, end, ongoing.StartTime, ongoing.EndTime) &&
			sendersCanCollide(sender, ongoing.Sender) {
			ongoing.Collided = true
			tx.Collided = true
			log.Printf("[Network] Collision detected between sender %s and sender %s for msg %s.\n",
				sender.GetID(), ongoing.Sender.GetID(), msg.GetID())
		}
	}
	net.mu.Unlock()

	// Schedule delivery after LoRaAirTime.
	time.AfterFunc(LoRaAirTime, func() {
		net.mu.Lock()
		defer net.mu.Unlock()

		// Remove the transmission record.
		delete(net.transmissions, msg.GetID())

		// if tx.Collided {
		// 	log.Printf("[Collision Drop] Message %s from node %s dropped due to collision.\n",
		// 		msg.GetID(), sender.GetID())
		// 	return
		// }

		// Deliver message using the pre-filtered recipients and a detailed collision check.
		net.deliverIfNoCollision(tx, sender)
	})
}

// UnicastMessage simulates a direct unicast from sender to msg.To().
func (net *networkImpl) UnicastMessage(msg message.IMessage, sender mesh.INode) {
	net.mu.RLock()
	defer net.mu.RUnlock()

	to := msg.GetTo()
	if receiver, ok := net.nodes[to]; ok {
		if net.IsInRange(sender, receiver) {
			ndChan := net.getNodeChannel(receiver)
			ndChan <- msg
		} else {
			log.Printf("[Network] Node %q is out of range for node %q.\n",
				sender.GetID(), to)
		}
	} else {
		log.Printf("[Network] Node %q tried to send to unknown node %q. Continuing anyway...\n",
			sender.GetID(), to)
	}
}

// addNode inserts the node into the map, starts its goroutine, and triggers a broadcast HELLO.
func (net *networkImpl) addNode(n mesh.INode) {
	net.mu.Lock()
	net.nodes[n.GetID()] = n
	net.mu.Unlock()



	log.Printf("[sim] Node %s: joining network.\n", n.GetID())
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
		// Print out the details of the leavign node
		log.Printf("[sim] Node %s: leaving network.\n", nodeID)
		net.nodes[nodeID].PrintNodeDetails()
		// Remove the node from the map
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

// Check if a node is in range to recieve signal from another node
func (net *networkImpl) IsInRange(node1 mesh.INode, node2 mesh.INode) bool {
	return node1.GetPosition().DistanceTo(node2.GetPosition()) <= maxRange
}
