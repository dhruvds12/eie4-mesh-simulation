package network

import (
	"fmt"
	"log"
	"sync"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
)

const (
	maxRange          = 1400.0                 // Maximum range (meters)
	LoRaAirTime       = 300 * time.Millisecond // Duration a transmission is on air
	PruneInterval     = 3 * time.Second        // How often to run the pruner
	TransmissionGrace = 500 * time.Millisecond // How long after end time to keep transmissions
	// TransmissionGrace required as txs are handled one by one if 1 transmission starts and another starts <300ms after the
	// first transmission would be removed before the check for a collision is completed therefore 2nd transmission would
	// also be delivered
)

// Struct to hold the transmission details
type Transmission struct {
	PacketID   uint32
	Packet     []byte
	Sender     mesh.INode
	StartTime  time.Time
	EndTime    time.Time
	Recipients []mesh.INode
	Active     bool
}

// timesOverlap returns true if the intervals [s1,e1) and [s2,e2) overlap.
func timesOverlap(s1, e1, s2, e2 time.Time) bool {
	return s1.Before(e2) && s2.Before(e1)
}

// sendersCanCollide returns true if the distance between two senders is less than 2*maxRange.
func sendersCanCollide(senderA, senderB mesh.INode) bool {
	dist := senderA.GetPosition().DistanceTo(senderB.GetPosition())
	return dist <= (2 * maxRange)
}

type networkImpl struct {
	mu    sync.RWMutex
	nodes map[uint32]mesh.INode

	joinRequests  chan mesh.INode
	leaveRequests chan uint32

	transmissions map[uint32]*Transmission

	quitPruner chan struct{}

	eventBus *eventBus.EventBus
}

// NewNetwork creates a new instance of the network.
func NewNetwork(bus *eventBus.EventBus) mesh.INetwork {
	net := &networkImpl{
		nodes:         make(map[uint32]mesh.INode),
		joinRequests:  make(chan mesh.INode),
		leaveRequests: make(chan uint32),
		transmissions: make(map[uint32]*Transmission),
		quitPruner:    make(chan struct{}),
		eventBus:      bus,
	}
	// Start the pruner goroutine.
	go net.runPruner()
	return net
}

// runPruner runs periodically (every PruneInterval) to remove transmissions that ended more than TransmissionGrace ago.
func (net *networkImpl) runPruner() {
	ticker := time.NewTicker(PruneInterval)
	defer ticker.Stop()
	for {
		select {
		case <-net.quitPruner:
			return
		case <-ticker.C:
			net.pruneTransmissions()
		}
	}
}

// pruneTransmissions iterates over the transmissions map and removes any transmission whose EndTime is more than TransmissionGrace ago.
func (net *networkImpl) pruneTransmissions() {
	net.mu.Lock()
	defer net.mu.Unlock()
	now := time.Now()
	for msgID, tx := range net.transmissions {
		// If the transmission ended more than TransmissionGrace ago, remove it.
		if now.After(tx.EndTime.Add(TransmissionGrace)) {
			// For debugging, you might also deliver to any remaining recipients before deletion if desired.
			delete(net.transmissions, msgID)
			log.Printf("[Pruner] Removed transmission %d (ended at %v).\n", msgID, tx.EndTime)
		}
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
func (net *networkImpl) Leave(nodeID uint32) {
	net.leaveRequests <- nodeID
}

func (net *networkImpl) LeaveAll() {
	for id := range net.nodes {
		net.leaveRequests <- id
	}
}

// / deliverIfNoCollision checks for each recipient in tx.Recipients whether that recipient
// sees any overlapping transmission. If so, delivery is skipped for that node; otherwise, delivered.
func (net *networkImpl) deliverIfNoCollision(tx *Transmission, sender mesh.INode) {
	// For each recipient from our pre-filtered list:
	// set inactive
	net.mu.Lock()
	tx.Active = false
	net.mu.Unlock()

	// Now, use a read lock for iterating over transmissions.
	net.mu.RLock()
	// Copy the relevant data from net.transmissions to minimise the lock duration.
	transmissionsSnapshot := make([]*Transmission, 0, len(net.transmissions))
	for _, otherTx := range net.transmissions {
		transmissionsSnapshot = append(transmissionsSnapshot, otherTx)
	}
	net.mu.RUnlock()

	for _, nd := range tx.Recipients {
		// Check again that the node is still in range (e.g. if nodes move).
		if !net.IsInRange(sender, nd) {
			log.Printf("[Network] Node %d is no longer in range for msg %d.\n", nd.GetID(), tx.PacketID)
			continue
		}

		overlappingCount := 0
		// Now, iterate over all transmissions in the global map.
		// (Because we do not delete finished transmissions immediately, even later transmissions
		// that started after tx but finished later will still be visible.)
		for _, otherTx := range transmissionsSnapshot {
			// Skip self.
			if otherTx == tx {
				continue
			}
			// Check if the transmission intervals overlap.
			if timesOverlap(tx.StartTime, tx.EndTime, otherTx.StartTime, otherTx.EndTime) {
				// Check if nd is in range of the other transmitter.
				if net.IsInRange(otherTx.Sender, nd) {
					overlappingCount++
				}
			}
		}

		// If more than zero overlapping transmissions affect this node, drop delivery.
		if overlappingCount > 0 {
			log.Printf("[Collision] At node %d, skipping delivery of msg %d due to %d overlapping transmission(s).\n",
				nd.GetID(), tx.PacketID, overlappingCount)
		} else {
			nd.GetMessageChan() <- tx.Packet
			log.Printf("[Network] Delivered msg %d to node %d.\n", tx.PacketID, nd.GetID())
		}
	}
}

// BroadcastMessage simulates a broadcast transmission. It pre-filters the recipient nodes
// that are in range of the sender at the start of transmission and creates a Transmission record.
// The transmission record is kept in the global map until it is pruned by the pruner goroutine.
func (net *networkImpl) BroadcastMessage(packet []byte, sender mesh.INode, packetID uint32) {
	net.mu.Lock()

	start := time.Now()
	end := start.Add(LoRaAirTime)

	// Pre-filter nodes: build a list of nodes that are in range of the sender at transmission start.
	var recipients []mesh.INode
	for id, nd := range net.nodes {
		if id == sender.GetID() {
			continue
		}

		if !sender.IsVirtual() && !nd.IsVirtual() {
			continue
		}
		if net.IsInRange(sender, nd) {
			recipients = append(recipients, nd)
			log.Printf("[Network] Node %d pre-filtered as recipient for msg %d.\n", id, packetID)
		}
	}

	// Create a transmission record.
	tx := &Transmission{
		PacketID:   packetID,
		Packet:     packet,
		Sender:     sender,
		StartTime:  start,
		EndTime:    end,
		Recipients: recipients,
		Active:     true,
	}

	// Store the transmission.
	net.transmissions[packetID] = tx

	net.mu.Unlock()

	// We schedule a callback with time.AfterFunc to deliver the message as soon as its airtime is finished.
	// Note: We do not delete the transmission here. The pruner will remove it after TransmissionGrace has elapsed.
	time.AfterFunc(LoRaAirTime, func() {
		// Deliver to recipients using a detailed per-node collision check.
		net.deliverIfNoCollision(tx, sender)
	})
}

// UnicastMessage simulates a direct unicast from sender to msg.To().
func (net *networkImpl) UnicastMessage(msg []byte, sender mesh.INode, packetID uint32, to uint32) {
	net.mu.RLock()
	defer net.mu.RUnlock()

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

	log.Printf("[sim] Node %d: joining network.\n", n.GetID())
	go n.Run(net)
}

// removeNode signals the node to stop and removes it from the map.
func (net *networkImpl) removeNode(nodeID uint32) {
	net.mu.Lock()
	if nd, ok := net.nodes[nodeID]; ok {
		// Close the node's quit channel if there's a direct handle
		// We don't have a direct reference to 'quit', so let's do a type check
		if ni, ok := nd.(interface{ QuitChan() chan struct{} }); ok {
			close(ni.QuitChan())
		}
		// Print out the details of the leavign node
		log.Printf("[sim] Node %d: leaving network.\n", nodeID)
		net.nodes[nodeID].PrintNodeDetails()
		// Remove the node from the map
		delete(net.nodes, nodeID)
	}
	net.mu.Unlock()
}

// getNodeChannel is a helper to get the 'messages' channel from the node.
// Because node.INode is an interface, we do a type assertion.
func (net *networkImpl) getNodeChannel(n mesh.INode) chan []byte {
	type channelGetter interface {
		GetMessageChan() chan []byte
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

// Check if the channel if free for transmission
// Checks each transmission in the map to see if the node is within range of any on going transmission
// Used as part of CSMA/CA
func (net *networkImpl) IsChannelFree(node mesh.INode) bool {
	net.mu.RLock()
	defer net.mu.RUnlock()
	for _, tx := range net.transmissions {
		if tx.Active && net.IsInRange(node, tx.Sender) {
			return false
		}
	}
	return true
}

// get node from map
func (net *networkImpl) GetNode(nodeId uint32) (mesh.INode, error) {
	net.mu.RLock()
	defer net.mu.RUnlock()
	if nd, ok := net.nodes[nodeId]; ok {
		return nd, nil
	}
	return nil, fmt.Errorf("node with id %d not found", nodeId)
}
