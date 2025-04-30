package routing

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/packet"
)

/*
TODO:
- Implement a timeout for routes
-Implement an ack for data packets (check what meshtastic does)
-Implement RERR (route error) messages when route is broken
*/

// RouteEntry stores route info
type RouteEntry struct {
	Destination uint32
	NextHop     uint32
	HopCount    int
}

// Used to store transactions that are pending ACKs
type PendingTx struct {
	MsgID               uint32
	Dest                uint32 // destination of the data
	PotentialBrokenNode uint32 // (the broken node)
	Origin              uint32 // original source of the data
	ExpiryTime          time.Time
}

type UserEntry struct {
	NodeID   uint32 // home node
	Seq      uint8  // helps to tell us what information to trust ie if received seq is > than current replace if not ignore
	LastSeen time.Time
}

type DataQueueEntry struct {
	packetType uint8
	payload    string
	sendUserID uint32
	destUserID uint32
}

type userMessageQueueEntry struct {
	senderID uint32
	payload  string
}

// AODVRouter is a per-node router
type AODVRouter struct {
	ownerID          uint32
	routeTable       map[uint32]*RouteEntry             // key = destination ID
	seenMsgIDs       map[uint32]bool                    // deduplicate RREQ/RREP
	dataQueue        map[uint32][]DataQueueEntry        // queue of data to send after route is established //TODO: NEED TO CHANGE TO UINT32
	userMessageQueue map[uint32][]userMessageQueueEntry // queue of data to send after route is established //TODO: NEED TO CHANGE TO UINT32
	queueMu          sync.RWMutex
	pendingTxs       map[uint32]PendingTx
	quitChan         chan struct{}
	eventBus         *eventBus.EventBus
	gut              map[uint32]UserEntry
	gutMu            sync.RWMutex
	lastUsersShadow  map[uint32]bool
}

// NewAODVRouter constructs a router for a specific node
func NewAODVRouter(ownerID uint32, bus *eventBus.EventBus) *AODVRouter {
	return &AODVRouter{
		ownerID:          ownerID,
		routeTable:       make(map[uint32]*RouteEntry), // TODO: implement a timeout for routes
		seenMsgIDs:       make(map[uint32]bool),
		dataQueue:        make(map[uint32][]DataQueueEntry),
		pendingTxs:       make(map[uint32]PendingTx),
		quitChan:         make(chan struct{}),
		eventBus:         bus,
		gut:              make(map[uint32]UserEntry), // global user table -> has the location of a user in the mesh network
		userMessageQueue: make(map[uint32][]userMessageQueueEntry),
		lastUsersShadow:  make(map[uint32]bool),
	}
}

// ------------------------------------------------------------
// runPendingTxChecker periodically checks pendingTxs for expired entries
// If expired, we assume we never overheard the forward => route is broken

func (r *AODVRouter) StartPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	go r.runPendingTxChecker(net, node)
}

// Checks if a transaction that has been sent has not been ACKed within a certain time
// If not, the route is considered broken
func (r *AODVRouter) runPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	ticker := time.NewTicker(1 * time.Second) // check every 1s
	defer ticker.Stop()

	for {
		select {
		case <-r.quitChan:
			return
		case <-ticker.C:
			now := time.Now()
			for msgID, tx := range r.pendingTxs {
				if now.After(tx.ExpiryTime) {
					log.Printf("[Timeout] Node %d: pendingTx expired for msgID=%d => route to %d is considered broken",
						r.ownerID, msgID, tx.Dest)

					// Remove from pendingTxs
					delete(r.pendingTxs, msgID)

					// Invalidate routes -> why is this nil //TODO:Resolve this
					r.InvalidateRoutes(tx.PotentialBrokenNode, tx.Dest, 0)

					route, found := r.routeTable[tx.Origin]
					if found {

						// If we have net & node references, we can send RERR
						if net != nil && node != nil {
							// We'll craft a minimal sendRERR. We can guess the original source as r.ownerID,
							// or store it in PendingTx if you want the actual source
							r.sendRERR(net, node, route.NextHop, tx.Dest, tx.PotentialBrokenNode, msgID, tx.Origin)
						}
					}
				}
			}
		}
	}
}

func (r *AODVRouter) StartBroadcastTicker(net mesh.INetwork, node mesh.INode) {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.quitChan:
				return
			case <-t.C:
				r.SendDiffBroadcastInfo(net, node)
			}
		}
	}()
}

// Stop the pendingTxChecker
func (r *AODVRouter) StopPendingTxChecker() {
	close(r.quitChan)
}

// -- IRouter methods --

func (r *AODVRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uint32, payload string) {
	// Check if we already have a route
	entry, hasRoute := r.routeTable[destID]
	if !hasRoute {
		// Initiate RREQ
		log.Printf("[sim] Node %d (router) -> no route for %d, initiating RREQ.\n", r.ownerID, destID)
		dqe := DataQueueEntry{
			packetType: packet.PKT_DATA,
			payload:    payload,
			sendUserID: 0,
			destUserID: 0,
		}
		r.dataQueue[destID] = append(r.dataQueue[destID], dqe)
		r.initiateRREQ(net, sender, destID)
		return
	}

	nextHop := entry.NextHop
	payloadBytes := []byte(payload)
	completePacket, packetID, err := packet.CreateDataPacket(r.ownerID, destID, nextHop, 0, payloadBytes)
	if err != nil {
		log.Printf("[sim] Node %d: failed to create data packet: %v\n", r.ownerID, err)
		return
	}

	// msgID2 := fmt.Sprintf("data-%s-%s-%d", r.ownerID, destID, time.Now().UnixNano())
	// dataMsg := &message.Message{
	// 	Type:    message.MsgData,
	// 	From:    r.ownerID,
	// 	Origin:  r.ownerID,
	// 	To:      nextHop, //Todo: this could be nextHop
	// 	Dest:    destID,
	// 	ID:      msgID2,
	// 	Payload: payload,
	// }

	log.Printf("[sim] Node %d (router) -> forwarding data to %d via %d\n", r.ownerID, destID, nextHop)
	net.BroadcastMessage(completePacket, sender, packetID)

	if destID != nextHop {
		expire := time.Now().Add(10 * time.Second) // e.g. 3s
		r.pendingTxs[packetID] = PendingTx{
			MsgID:               packetID,
			Dest:                destID,
			PotentialBrokenNode: nextHop,
			Origin:              r.ownerID,
			ExpiryTime:          expire,
		}

	}

}

func (r *AODVRouter) SendUserMessage(net mesh.INetwork, sender mesh.INode, sendUserID, destUserID uint32, payload string) {
	// check GUT
	userEntry, ok := r.hasUserEntry(destUserID)
	if !ok {
		// save the message
		r.saveUsermessage(sendUserID, destUserID, payload)
		r.initiateUREQ(net, sender, destUserID)
		return
	}

	entry, hasRoute := r.routeTable[userEntry.NodeID]
	if !hasRoute {
		// Initiate RREQ
		log.Printf("[sim] Node %d (router) sendUserMessage -> no route for %d, initiating RREQ.\n", r.ownerID, userEntry.NodeID)
		dqe := DataQueueEntry{
			packetType: packet.PKT_USER_MSG,
			payload:    payload,
			sendUserID: sendUserID,
			destUserID: destUserID,
		}
		r.dataQueue[userEntry.NodeID] = append(r.dataQueue[userEntry.NodeID], dqe)
		r.initiateRREQ(net, sender, userEntry.NodeID)
		return
	}

	nextHop := entry.NextHop
	payloadBytes := []byte(payload)
	completePacket, packetID, err := packet.CreateUSERMessagePacket(r.ownerID, sendUserID, destUserID, userEntry.NodeID, nextHop, 0, payloadBytes)
	if err != nil {
		log.Printf("[sim] Node %d: failed to create data packet: %v\n", r.ownerID, err)
		return
	}

	log.Printf("[sim] Node %d (router) -> forwarding user message to %d via %d\n", r.ownerID, userEntry.NodeID, nextHop)
	net.BroadcastMessage(completePacket, sender, packetID)

	if userEntry.NodeID != nextHop {
		expire := time.Now().Add(10 * time.Second) // e.g. 3s
		r.pendingTxs[packetID] = PendingTx{
			MsgID:               packetID,
			Dest:                userEntry.NodeID,
			PotentialBrokenNode: nextHop,
			Origin:              r.ownerID,
			ExpiryTime:          expire,
		}

	}

}

func (r *AODVRouter) SendDataCSMA(net mesh.INetwork, sender mesh.INode, destID uint32, payload string) {
	backoff := 100 * time.Millisecond
	// Check if the channel is busy
	for !net.IsChannelFree(sender) {
		waitTime := time.Duration(1+rand.Intn(int(backoff/time.Millisecond))) * time.Millisecond
		log.Printf("[CSMA] Node %d: Channel busy. Waiting for %v before retrying.\n", r.ownerID, waitTime)
		time.Sleep(waitTime)
		backoff *= 2
		if backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}
	log.Printf("[CSMA] Node %d: Channel is free. Sending data.\n", r.ownerID)
	r.SendData(net, sender, destID, payload)

}

// HandleMessage is called when the node receives any message
// TODO: repeated logic should be removed?????
func (r *AODVRouter) HandleMessage(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(receivedPacket); err != nil {
		log.Printf("Node %d: failed to deserialize BaseHeader: %v", r.ownerID, err)
		return
	}
	switch bh.PacketType {
	case packet.PKT_BROADCAST_INFO:
		log.Printf("Node %d: received BroadcastInfo\n", r.ownerID)
		r.handleBroadcastInfo(net, node, receivedPacket)
	case packet.PKT_RREQ:
		// All nodes who recieve should handle
		log.Printf("Node %d: received PKT_RREQ\n", r.ownerID)
		r.handleRREQ(net, node, receivedPacket)
	case packet.PKT_RREP:
		// Only the intended recipient should handle
		log.Printf("Node %d: received PKT_RREP\n", r.ownerID)
		if bh.DestNodeID == r.ownerID {
			r.handleRREP(net, node, receivedPacket)
		}
	case packet.PKT_RERR:
		// All nodes who recieve should handle
		log.Printf("Node %d: received PKT_RERR\n", r.ownerID)
		r.handleRERR(net, node, receivedPacket)
	case packet.PKT_ACK: // was data ack can be generalised
		// Only the intended recipient should handle
		if bh.DestNodeID == r.ownerID {
			log.Printf("Node %d: received DATA_ACK\n", r.ownerID)
			//TODO: Explicit acks disabled
			// r.HandleDataAck(receivedPacket)
		}
	case packet.PKT_DATA:
		// Overhearing logic for implicit ACKs
		if _, ok := r.pendingTxs[bh.PacketID]; ok {
			// ack
			log.Printf("{Implicit ACK}  Node %d: overheard forward from %d => implicit ack for msgID=%d",
				r.ownerID, bh.SrcNodeID, bh.PacketID)
			delete(r.pendingTxs, bh.PacketID)
		}
		// Only the intended recipient should handle
		if bh.DestNodeID == r.ownerID {
			r.handleDataForward(net, node, receivedPacket)
		}
	case packet.PKT_UREQ:
		r.handleUREQ(net, node, receivedPacket)
	case packet.PKT_UREP:
		r.handleUREP(net, node, receivedPacket)
	case packet.PKT_UERR:
		r.handleUERR(net, node, receivedPacket)
	case packet.PKT_USER_MSG:
		r.handleUserMessage(net, node, receivedPacket)
	default:
		// not a routing message
		log.Printf("[sim] Node %d (router) -> received non-routing message: %d\n", r.ownerID, bh.PacketType)
	}
}

// AddDirectNeighbor is called by the node when it discovers a new neighbor
func (r *AODVRouter) AddDirectNeighbor(nodeID, neighborID uint32) {
	// only do this if nodeID == r.ownerID
	if nodeID != r.ownerID {
		return
	}
	// If we don't have a route, or if this is a shorter route
	existing, exists := r.routeTable[neighborID]
	if !exists || (exists && existing.HopCount > 1) {

		r.AddRouteEntry(neighborID, neighborID, 1)
		log.Printf("[sim] [routing table] Node %d (router) -> direct neighbor: %d\n", r.ownerID, neighborID)
	}
}

func (r *AODVRouter) PrintRoutingTable() {
	fmt.Printf("Node %d (router) -> routing table:\n", r.ownerID)
	for dest, route := range r.routeTable {
		fmt.Printf("   %d -> via %d (hop count %d)\n", dest, route.NextHop, route.HopCount)
	}
}

func (r *AODVRouter) SendBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	nodeId := node.GetID()
	// Create a unique broadcast ID to deduplicate

	infoPacket, packetID, err := packet.CreateBroadcastInfoPacket(nodeId, nodeId, node.GetConnectedUsers(), 0)
	if err != nil {
		log.Fatalf("Node %d: failed to creare Info packet: %v", nodeId, err)

	}
	// net.BroadcastMessage(m, node)
	r.BroadcastMessageCSMA(net, node, infoPacket, packetID)
}

func (r *AODVRouter) SendDiffBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	cur := node.GetConnectedUsers()
	now := make(map[uint32]bool, len(cur))
	for _, u := range cur {
		now[u] = true
	}

	var added, removed []uint32
	for u := range now {
		if !r.lastUsersShadow[u] {
			added = append(added, u)
		}
	}
	for u := range r.lastUsersShadow {
		if !now[u] {
			removed = append(removed, u)
		}
	}

	pkt, pid, err := packet.CreateDiffBroadcastInfoPacket(
		r.ownerID, r.ownerID, added, removed, 0,
	)
	if err == nil {
		r.BroadcastMessageCSMA(net, node, pkt, pid)
	}
	r.lastUsersShadow = now
}

func (r *AODVRouter) AddRouteEntry(dest, nextHop uint32, hopCount int) {
	re := RouteEntry{
		Destination: dest,
		NextHop:     nextHop,
		HopCount:    hopCount,
	}

	r.routeTable[dest] = &re

	r.eventBus.Publish(eventBus.Event{
		Type:   eventBus.EventAddRouteEntry,
		NodeID: r.ownerID,
		RoutingTableEntry: eventBus.RouteEntry{
			Destination: dest,
			NextHop:     nextHop,
			HopCount:    re.HopCount,
		},
		Timestamp: time.Now(),
	})

}

func (r *AODVRouter) RemoveRouteEntry(dest uint32) {
	route := r.routeTable[dest]

	delete(r.routeTable, dest)
	r.eventBus.Publish(eventBus.Event{
		Type:   eventBus.EventRemoveRouteEntry,
		NodeID: r.ownerID,
		RoutingTableEntry: eventBus.RouteEntry{
			Destination: route.Destination,
			NextHop:     route.NextHop,
			HopCount:    route.HopCount,
		},
		Timestamp: time.Now(),
	})

}

// Check that channel is free before sending data to the network
// Will call the broadcast function in the network to send the message to all nodes
func (r *AODVRouter) BroadcastMessageCSMA(net mesh.INetwork, sender mesh.INode, sendPacket []byte, packetID uint32) {
	backoff := 100 * time.Millisecond
	// Check if the channel is busy
	for !net.IsChannelFree(sender) {
		waitTime := time.Duration(1+rand.Intn(int(backoff/time.Millisecond))) * time.Millisecond
		log.Printf("[CSMA] Node %d: Channel busy. Waiting for %v before retrying.\n", r.ownerID, waitTime)
		time.Sleep(waitTime)
		backoff *= 2
		if backoff > 2*time.Second {
			backoff = 2 * time.Second
		}
	}
	log.Printf("[CSMA] Node %d: Channel is free. Broadcasting message.\n", r.ownerID)
	net.BroadcastMessage(sendPacket, sender, packetID)
}

// -- Private AODV logic --
// handleBroadcastInfo processes a HELLO broadcast message.
func (r *AODVRouter) handleBroadcastInfo(net mesh.INetwork, node mesh.INode, buf []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(buf); err != nil {
		return
	}
	ofs := 16

	var dh packet.DiffBroadcastInfoHeader
	if err := dh.Deserialise(buf[ofs:]); err != nil {
		return
	}
	ofs += 8

	total := int(dh.NumAdded) + int(dh.NumRemoved)
	if len(buf) < ofs+4*total {
		return
	}
	ids := make([]uint32, total)
	for i := 0; i < total; i++ {
		ids[i] = binary.LittleEndian.Uint32(buf[ofs+i*4:])
	}

	added := ids[:dh.NumAdded]
	removed := ids[dh.NumAdded:]

	r.AddDirectNeighbor(node.GetID(), bh.SrcNodeID)
	if dh.OriginNodeID != r.ownerID {
		r.maybeAddRoute(dh.OriginNodeID, bh.SrcNodeID, int(bh.HopCount)+1)
	}
	for _, u := range added {
		r.addToGUT(u, dh.OriginNodeID)
	}
	for _, u := range removed {
		r.removeUserEntry(u, dh.OriginNodeID)
	}

	if dh.OriginNodeID != r.ownerID && bh.HopCount < packet.MAX_HOPS {
		fwd, pid, _ := packet.CreateDiffBroadcastInfoPacket(
			r.ownerID, dh.OriginNodeID, added, removed,
			bh.HopCount+1, bh.PacketID,
		)
		r.BroadcastMessageCSMA(net, node, fwd, pid)
	}
}

func (r *AODVRouter) initiateRREQ(net mesh.INetwork, sender mesh.INode, destID uint32) {
	rreqPacket, packetID, err := packet.CreateRREQPacket(r.ownerID, destID, r.ownerID, 0)
	if err != nil {
		log.Printf("Error in initRREQ with CreateRREQPacket: %q", err)
		return
	}
	log.Printf("[sim] [RREQ init] Node %d (router) -> initiating RREQ for %d (hop count %d)\n", r.ownerID, destID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
}

// Every Node needs to handle this
func (r *AODVRouter) handleRREQ(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, rh, err := packet.DeserialiseRREQPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		log.Printf("Node %d: ignoring duplicate RREQ.\n", r.ownerID)
		return
	}
	r.seenMsgIDs[bh.PacketID] = true
	// Add reverse route to RREQ source
	r.maybeAddRoute(rh.OriginNodeID, bh.SrcNodeID, int(bh.HopCount)+1)

	// if I'm the destination, send RREP
	if r.ownerID == rh.RREQDestNodeID {
		log.Printf("[sim] Node %d: RREQ arrived at destination.\n", r.ownerID)
		r.sendRREP(net, node, rh.RREQDestNodeID, rh.OriginNodeID, 0) // Should this reset to 0 (yes)
		return
	}

	// If we have a route to the destination, we can send RREP
	if route, ok := r.routeTable[rh.RREQDestNodeID]; ok {
		r.sendRREP(net, node, rh.RREQDestNodeID, rh.OriginNodeID, route.HopCount)
		return
	}

	fwdRREQ, packetId, err := packet.CreateRREQPacket(r.ownerID, rh.RREQDestNodeID, rh.OriginNodeID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] [RREQ FORWARD] Node %d: forwarding RREQ for %d (hop count %d)\n", r.ownerID, rh.RREQDestNodeID, bh.HopCount)
	// net.BroadcastMessage(fwdMsg, node)
	r.BroadcastMessageCSMA(net, node, fwdRREQ, packetId)
}

// ONLY use to initate rrep
func (r *AODVRouter) sendRREP(net mesh.INetwork, node mesh.INode, destRREP, sourceRREP uint32, hopCount int) {
	// find route to 'source' in reverse direction
	reverseRoute := r.routeTable[sourceRREP]
	if reverseRoute == nil {
		log.Printf("Node %d: can't send RREP, no route to %d.\n", r.ownerID, sourceRREP)
		return
	}
	rrepPacket, packetID, err := packet.CreateRREPPacket(r.ownerID, destRREP, reverseRoute.NextHop, sourceRREP, 0, 0, uint8(hopCount))
	if err != nil {
		return
	}
	log.Printf("[sim] [RREP] Node %d: sending RREP to %d via %d current hop count: %d\n", r.ownerID, destRREP, reverseRoute.NextHop, hopCount)
	// net.BroadcastMessage(rrepMsg, node)
	r.BroadcastMessageCSMA(net, node, rrepPacket, packetID)
}

// Only node specified in the RREP message should handle this
func (r *AODVRouter) handleRREP(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise rrep
	bh, rreph, err := packet.DeserialiseRREPPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		return
	}
	r.seenMsgIDs[bh.PacketID] = true

	// Add forward route to ctrl.Source
	r.maybeAddRoute(rreph.RREPDestNodeID, bh.SrcNodeID, int(rreph.NumHops)+1)
	r.maybeAddRoute(bh.SrcNodeID, bh.SrcNodeID, 1)

	// if I'm the original route requester, done
	if r.ownerID == rreph.RREPDestNodeID {
		log.Printf("Node %d: route to %d established!\n", r.ownerID, rreph.OriginNodeID)
		// send any queued data
		for _, dqe := range r.dataQueue[rreph.OriginNodeID] {
			if dqe.packetType == packet.PKT_DATA {

				r.SendData(net, node, rreph.OriginNodeID, dqe.payload)
			}

			if dqe.packetType == packet.PKT_USER_MSG {
				r.SendUserMessage(net, node, dqe.sendUserID, dqe.destUserID, dqe.payload)
			}
		}
		delete(r.dataQueue, rreph.OriginNodeID)
		return
	}

	// else forward RREP
	reverseRoute := r.routeTable[rreph.RREPDestNodeID]
	if reverseRoute == nil {
		log.Printf("Node %d: got RREP but no route back to %d.\n", r.ownerID, rreph.RREPDestNodeID)
		return
	}

	rrepPacket, packetID, err := packet.CreateRREPPacket(r.ownerID, rreph.RREPDestNodeID, reverseRoute.Destination, rreph.OriginNodeID, 0, bh.HopCount+1, rreph.NumHops+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[RREP FORWARD] Node %d: forwarding RREP to %d via %d\n", r.ownerID, rreph.RREPDestNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdRrep, node)
	r.BroadcastMessageCSMA(net, node, rrepPacket, packetID)
}

// ONLY use for initial rerr not suitable for forwarding
func (r *AODVRouter) sendRERR(net mesh.INetwork, node mesh.INode, to uint32, dataDest uint32, brokenNode uint32, packetId uint32, messageSource uint32) {

	rerrPacket, packetID, err := packet.CreateRERRPacket(r.ownerID, to, r.ownerID, brokenNode, dataDest, packetId, messageSource, 0)
	if err != nil {
		return
	}

	r.seenMsgIDs[packetID] = true

	log.Printf("[sim] Node %d: sending RERR to %d about broken route for %d.\n", r.ownerID, to, dataDest)
	// net.BroadcastMessage(rerrMsg, node)
	r.BroadcastMessageCSMA(net, node, rerrPacket, packetID)
}

// Everyone who receives RERR should handle this (only intended recipient should forward to source) (source should not forward)
func (r *AODVRouter) handleRERR(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise bh and rerrHeader
	bh, rerrHeader, err := packet.DeserialiseRERRPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		return
	}
	r.seenMsgIDs[bh.PacketID] = true

	log.Printf("[sim] Node %d: received RERR => broken node: %d for dest %d\n", r.ownerID, rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID)

	// Invalidate routes
	r.InvalidateRoutes(rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID, bh.SrcNodeID)

	if r.ownerID != bh.DestNodeID {
		// Check if node is the intended target
		log.Printf("{RERR} Node %d: received RERR not intended for me.\n", r.ownerID)
		return
	}

	if r.ownerID == rerrHeader.SenderNodeID {
		log.Printf("{RERR} Node %d: received RERR for my own message, stopping here.\n", r.ownerID)
		return
	}

	entry, hasRoute := r.routeTable[rerrHeader.SenderNodeID]
	if !hasRoute {
		// No route to source, can't forward RERR
		log.Printf("[sim] {RERR FAIL} Node %d: no route to forward RERR destined for %d.\n", r.ownerID, rerrHeader.SenderNodeID)
		// TODO: might need to initiate RREQ to source
		return
	}

	// If we have a route to the source of the message, we can forward the RERR
	nexthop := entry.NextHop
	// r.sendRERR(net, node, nexthop, rc.MessageDest, rc.BrokenNode, rc.MessageId, rc.MessageSource)
	rerrPacket, packetID, err := packet.CreateRERRPacket(r.ownerID, nexthop, rerrHeader.ReporterNodeID, rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID, rerrHeader.OriginalPacketID, rerrHeader.SenderNodeID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] Node %d: sending RERR to %d about broken route for %d.\n", r.ownerID, nexthop, rerrHeader.OriginalDestNodeID)
	// net.BroadcastMessage(rerrMsg, node)
	r.BroadcastMessageCSMA(net, node, rerrPacket, packetID)
	// Message source is in the payload, use existing route to forward RERR
	// This is a simplification, in real AODV we might need to store the "previous hop" for each route

	// if I'm not the source, I forward RERR upstream
	// how do we know who is the source? We might store it or check who gave me data originally
	// For now, let's just forward to the route of rc.Dest's Source if we know it
	// or we can store the msg.GetFrom() as the "previous hop" and forward there
	// If I have a route to the original source of the data, we can forward
	// This part can vary depending on how you track the "source" of data

	// (Simplified) If we do want to forward RERR, we need to know the "previous hop".
	// We might just do nothing if we don't store that.
	// Real AODV would keep track of all active flows or have a route to the source.

	// if we are the original source, we might re-initiate RREQ.
	// But for simplicity, let's stop here.
}

// handleDataForward attempts to forward data if the node isn't the final dest
func (r *AODVRouter) handleDataForward(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise Data paacket
	bh, dh, payload, err := packet.DeserialiseDataPacket(receivedPacket)
	if err != nil {
		return
	}

	payloadString := string(payload)
	// If I'm the final destination, do nothing -> the node can "deliver" it
	if dh.FinalDestID == r.ownerID {
		log.Printf("[sim] Node %d: DATA arrived. Payload = %q\n", r.ownerID, payload)
		// Send an ACK back to the sender
		r.eventBus.Publish(eventBus.Event{
			Type:        eventBus.EventMessageDelivered,
			NodeID:      r.ownerID,
			Payload:     payloadString,
			OtherNodeID: bh.SrcNodeID,
			Timestamp:   time.Now(),
		})
		// TODO: this is a simplification as this should depend on the packet header -> should not always be sending an ack
		//TODO: Explicit ack disabled
		r.sendDataAck(net, node, bh.SrcNodeID, bh.PacketID)
		return
	}

	//TODO: Could this call SendData? (NO -> send data now only for initiation)

	// Otherwise, I should forward it if I have a route

	dest := dh.FinalDestID
	route, ok := r.routeTable[dest]
	if !ok {
		// This is in theory an unlikely case because the origin node should have initiated RREQ
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %d: no route to forward DATA destined for %d.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		// no route therefore, we need ot send RERR
		r.sendRERR(net, node, bh.SrcNodeID, dest, r.ownerID, bh.PacketID, bh.SrcNodeID)
		return
	}

	dataPacket, packetID, err := packet.CreateDataPacket(bh.SrcNodeID, dh.FinalDestID, route.NextHop, bh.HopCount+1, payload, bh.PacketID)
	if err != nil {
		return
	}

	log.Printf("[sim] Node %d: forwarding DATA from %d to %d via %d\n", r.ownerID, bh.SrcNodeID, dest, route.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	r.BroadcastMessageCSMA(net, node, dataPacket, packetID)

	// Implicit ACK: if the next hop is the intended recipient, we can assume the data was received
	if route.NextHop != dest {
		// log.Printf("{Implicit ACK} Node %d: overheard forward from %d => implicit ack for msgID=%d", r.ownerID, originID, msg.GetID())
		// TODO: need to wait for an explicit ACK request from sender (simplified)
		expire := time.Now().Add(3 * time.Second) // e.g. 3s
		r.pendingTxs[bh.PacketID] = PendingTx{
			MsgID:               packetID,
			Dest:                dest,
			PotentialBrokenNode: route.NextHop,
			Origin:              bh.SrcNodeID,
			ExpiryTime:          expire,
		}
		return
	}

	// // If the next hop is not the destination, we need to track the transaction by overhearing it
	// expire := time.Now().Add(3 * time.Second) // e.g. 3s
	// r.pendingTxs[bh.PacketID] = PendingTx{
	// 	MsgID:               bh.PacketID,
	// 	Dest:                dest,
	// 	PotentialBrokenNode: route.NextHop,
	// 	Origin:              bh.SrcNodeID,
	// 	ExpiryTime:          expire,
	// }

}

func (r *AODVRouter) handleUserMessage(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, umh, payload, err := packet.DeserialiseUSERMessagePacket(receivedPacket)
	if err != nil {
		return
	}

	payloadString := string(payload)
	if umh.ToNodeID == r.ownerID {
		// check that the user is at this node
		ok := node.HasConnectedUser(umh.ToUserID)
		if ok {
			log.Printf("[sim] Node %d: USER MESSAGE arrived for user %d from %d. Payload = %q\n", r.ownerID, umh.ToUserID, umh.FromUserID, payload)
			// Send an ACK back to the sender
			r.eventBus.Publish(eventBus.Event{
				Type:        eventBus.EventMessageDelivered, //TODO change to another type to represent a user message
				NodeID:      r.ownerID,
				Payload:     payloadString,
				OtherNodeID: bh.SrcNodeID,
				Timestamp:   time.Now(),
			})
			// TODO: this is a simplification as this should depend on the packet header -> should not always be sending an ack
			r.sendDataAck(net, node, bh.SrcNodeID, bh.PacketID)
			return
		}

		// find route back to sender to send UERR
		route, ok := r.routeTable[bh.SrcNodeID]
		if ok {

			r.initiateUERR(net, node, route.NextHop, bh.SrcNodeID, bh.PacketID, umh.ToUserID)
		} else {
			log.Println("[UERR FAILED] Could not send back to suer as I have no route back to sender - strange behaviour")
		}
		return
	}

	dest := umh.ToNodeID
	route, ok := r.routeTable[dest]
	if !ok {
		// This is in theory an unlikely case because the origin node should have initiated RREQ
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %d: no route to forward USER MESSAGE destined for %d.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		// no route therefore, we need ot send RERR
		r.sendRERR(net, node, bh.SrcNodeID, dest, r.ownerID, bh.PacketID, bh.SrcNodeID)
		return
	}

	userMessagePacket, packetID, err := packet.CreateUSERMessagePacket(bh.SrcNodeID, umh.FromUserID, umh.ToUserID, umh.ToNodeID, route.NextHop, bh.HopCount+1, payload, bh.PacketID)
	if err != nil {
		return
	}

	r.BroadcastMessageCSMA(net, node, userMessagePacket, packetID)

	if route.NextHop != dest {
		expire := time.Now().Add(3 * time.Second) // e.g. 3s
		r.pendingTxs[bh.PacketID] = PendingTx{
			MsgID:               bh.PacketID,
			Dest:                dest,
			PotentialBrokenNode: route.NextHop,
			Origin:              bh.SrcNodeID,
			ExpiryTime:          expire,
		}

	}

}

// Handle Data ACKs, should remove from pendingTxs
func (r *AODVRouter) HandleDataAck(receivedPacket []byte) {
	// Unpack the payload and remove from pendingTxs
	_, ack, err := packet.DeserialiseACKPacket(receivedPacket)

	if err != nil {
		return
	}

	// log.Printf("{ACK} Node %d: received ACK for msgID=%d\n", r.ownerID, ack.MsgID)

	if _, ok := r.pendingTxs[ack.OriginalPacketID]; ok {
		log.Printf("{ACK} Node %d: received ACK for msgID=%d\n", r.ownerID, ack.OriginalPacketID)
		delete(r.pendingTxs, ack.OriginalPacketID)
	}
}

func (r *AODVRouter) initiateUREQ(net mesh.INetwork, sender mesh.INode, targetUser uint32) {
	rreqPacket, packetID, err := packet.CreateUREQPacket(r.ownerID, r.ownerID, targetUser, 0)
	if err != nil {
		log.Printf("Error in initUREQ with CreateUREQPacket: %q", err)
		return
	}
	log.Printf("[sim] [UREQ init] Node %d (router) -> initiating UREQ for %d (hop count %d)\n", r.ownerID, targetUser, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
}

func (r *AODVRouter) initiateUREP(net mesh.INetwork, sender mesh.INode, destID, ureqOrigin uint32, targetUser uint32) {
	rreqPacket, packetID, err := packet.CreateUREPPacket(r.ownerID, destID, ureqOrigin, r.ownerID, targetUser, 0, 0)
	if err != nil {
		log.Printf("Error in initUREP with CreateUREPPacket: %q", err)
		return
	}
	log.Printf("[sim] [UREP init] Node %d (router) -> initiating UREP for %d (hop count %d)\n", r.ownerID, destID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
}

func (r *AODVRouter) initiateUERR(net mesh.INetwork, sender mesh.INode, destNodeID, originNodeID, originalPacketID uint32, UERRUserID uint32) {
	rreqPacket, packetID, err := packet.CreateUERRPacket(r.ownerID, destNodeID, UERRUserID, r.ownerID, originNodeID, originalPacketID)
	if err != nil {
		log.Printf("Error in initUERR with CreateUERRPacket: %q", err)
		return
	}
	log.Printf("[sim] [UERR init] Node %d (router) -> initiating UERR for %d (hop count %d)\n", r.ownerID, destNodeID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
}

func (r *AODVRouter) handleUREQ(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUREQPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		log.Printf("Node %d: ignoring duplicate UREQ.\n", r.ownerID)
		return
	}
	r.seenMsgIDs[bh.PacketID] = true

	// add routes

	r.maybeAddRoute(bh.SrcNodeID, bh.SrcNodeID, 1)
	r.maybeAddRoute(uh.OriginNodeID, bh.SrcNodeID, int(bh.HopCount)+1)

	if node.HasConnectedUser(uh.UREQUserID) {
		// user connected to me return UREQ
		log.Printf("[sim] [UREQ] Node %d: has user connected%d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount+1)
		reply, pid, _ := packet.CreateUREPPacket(r.ownerID, bh.SrcNodeID, uh.OriginNodeID, r.ownerID, uh.UREQUserID, 0, 0)
		r.BroadcastMessageCSMA(net, node, reply, pid)
		return
	}

	// If I have a path to user reply
	if n, ok := r.hasUserEntry(uh.UREQUserID); ok {
		// reply with UREP along reverse path
		log.Printf("[sim] [UREQ] Node %d: has ROUTE userr %d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount)
		reply, pid, _ := packet.CreateUREPPacket(r.ownerID, bh.SrcNodeID, uh.OriginNodeID, n.NodeID, uh.UREQUserID, 0, 0)
		r.BroadcastMessageCSMA(net, node, reply, pid)
		return
	}

	// Otherwise forward the rreq
	fwdUREQ, packetId, err := packet.CreateUREQPacket(r.ownerID, uh.OriginNodeID, uh.UREQUserID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] [UREQ FORWARD] Node %d: forwarding UREQ for %d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount)
	// net.BroadcastMessage(fwdMsg, node)
	r.BroadcastMessageCSMA(net, node, fwdUREQ, packetId)

}

func (r *AODVRouter) handleUREP(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUREPPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		log.Printf("Node %d: ignoring duplicate UREP.\n", r.ownerID)
		return
	}
	r.seenMsgIDs[bh.PacketID] = true

	if r.ownerID == uh.OriginNodeID {
		// store in the GUT
		r.addToGUT(uh.UREPUserID, uh.UREPDestNodeID)

		// add to router
		r.maybeAddRoute(uh.UREPDestNodeID, bh.SrcNodeID, int(bh.HopCount)+1)

		// do some thing like send the messages that were queued

		msgs := r.getUserMessages(uh.UREPUserID)
		for _, umqe := range msgs {
			log.Printf("[USER MESSAGE] Sending message to user %d from user %d", uh.UREPUserID, umqe.senderID)
			r.SendUserMessage(net, node, umqe.senderID, uh.UREPUserID, umqe.payload)
		}
		return

	}

	// find next hop from routing table - should have way back
	reverseRoute := r.routeTable[uh.UREPDestNodeID]
	if reverseRoute == nil {
		log.Printf("Node %d: got UREP but no route back to %d.\n", r.ownerID, uh.UREPDestNodeID)
		return
	}

	// Otherwise forward the rreq
	fwdUREP, packetId, err := packet.CreateUREPPacket(r.ownerID, reverseRoute.NextHop, uh.OriginNodeID, uh.UREPDestNodeID, uh.UREPUserID, 0, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[UREP FORWARD] Node %d: forwarding UREP to %d via %d\n", r.ownerID, uh.UREPDestNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	r.BroadcastMessageCSMA(net, node, fwdUREP, packetId)

}

func (r *AODVRouter) handleUERR(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUERRPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.seenMsgIDs[bh.PacketID] {
		log.Printf("Node %d: ignoring duplicate UERR.\n", r.ownerID)
		return
	}
	r.seenMsgIDs[bh.PacketID] = true

	if r.ownerID == uh.OriginNodeID {
		r.removeUserEntry(uh.UserID, uh.NodeID)
		log.Printf("[UERR RECEIVED] Node %d received a uerr ", r.ownerID)
		// should not remove route as a UERR is only tiggered
		delete(r.pendingTxs, uh.OriginalPacketID)
		return
	}

	// else forward the message to destination
	reverseRoute := r.routeTable[uh.OriginNodeID]
	if reverseRoute == nil {
		log.Printf("Node %d: got UERR but no route back to %d.\n", r.ownerID, uh.OriginNodeID)
		return
	}

	// Otherwise forward the rreq
	fwdUREP, packetId, err := packet.CreateUERRPacket(r.ownerID, reverseRoute.NextHop, uh.UserID, uh.NodeID, uh.OriginalPacketID, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[UERR FORWARD] Node %d: forwarding UERR to %d via %d\n", r.ownerID, uh.OriginNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	r.BroadcastMessageCSMA(net, node, fwdUREP, packetId)

}

func (r *AODVRouter) InvalidateRoutes(brokenNode uint32, dest uint32, sender uint32) {
	// remove route to destination node if it goes through the sender
	if sender != 0 {
		if route, ok := r.routeTable[dest]; ok {
			if route.NextHop == sender {
				log.Printf("Node %d: removing route to %d because it goes through broken node %d.\n", r.ownerID, dest, sender)
				delete(r.routeTable, dest)

				r.eventBus.Publish(eventBus.Event{
					Type:   eventBus.EventRemoveRouteEntry,
					NodeID: r.ownerID,
					RoutingTableEntry: eventBus.RouteEntry{
						Destination: route.Destination,
						NextHop:     route.NextHop,
						HopCount:    route.HopCount,
					},
					Timestamp: time.Now(),
				})
			}
		}
	}

	// Remove any direct route to the broken node.
	if route, ok := r.routeTable[brokenNode]; ok {
		log.Printf("Node %d: removing direct route to broken node %d.\n", r.ownerID, brokenNode)
		delete(r.routeTable, brokenNode)
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventRemoveRouteEntry,
			NodeID: r.ownerID,
			RoutingTableEntry: eventBus.RouteEntry{
				Destination: route.Destination,
				NextHop:     route.NextHop,
				HopCount:    route.HopCount,
			},
			Timestamp: time.Now(),
		})
	}
	// Iterate through the routing table.
	for dest, route := range r.routeTable {
		if route.NextHop == brokenNode {
			log.Printf("Node %d: invalidating route to %d because NextHop %d is broken.\n", r.ownerID, dest, brokenNode)
			delete(r.routeTable, dest)
			r.eventBus.Publish(eventBus.Event{
				Type:   eventBus.EventRemoveRouteEntry,
				NodeID: r.ownerID,
				RoutingTableEntry: eventBus.RouteEntry{
					Destination: route.Destination,
					NextHop:     route.NextHop,
					HopCount:    route.HopCount,
				},
				Timestamp: time.Now(),
			})
		}
	}
}

// send ack for data packets
func (r *AODVRouter) sendDataAck(net mesh.INetwork, node mesh.INode, to uint32, prevMsgId uint32) {

	if true {
		log.Println("[sim] disabled explicit acks")
		return
	}

	route, ok := r.routeTable[to]
	if !ok {
		log.Printf("[sim] Node %d: no route to send data ack destined for %d.\n", r.ownerID, to)
		return
	}

	ackPacket, packetID, err := packet.CreateACKPacket(r.ownerID, to, route.NextHop, prevMsgId, 0)

	if err != nil {
		return
	}

	log.Printf("[sim] Node %d: sending DATA_ACK to %d\n", r.ownerID, to)
	r.BroadcastMessageCSMA(net, node, ackPacket, packetID)
}

// maybeAddRoute updates route if shorter
func (r *AODVRouter) maybeAddRoute(dest, nextHop uint32, hopCount int) {
	exist, ok := r.routeTable[dest]
	if !ok || hopCount < exist.HopCount {
		// log an update to the routing table
		log.Printf("[sim] [routing table] Node %d (router) -> updated route to %d via %d (hop count %d)\n", r.ownerID, dest, nextHop, hopCount)

		r.AddRouteEntry(dest, nextHop, hopCount)
	}
}

func (r *AODVRouter) addToGUT(userId, nodeId uint32) {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	r.gut[userId] = UserEntry{
		NodeID:   nodeId,
		Seq:      0, // set to 0 for now
		LastSeen: time.Now(),
	}
}

func (r *AODVRouter) hasUserEntry(userId uint32) (UserEntry, bool) {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	if userEntry, ok := r.gut[userId]; ok {
		return userEntry, ok
	}

	return UserEntry{}, false

}

func (r *AODVRouter) removeUserEntry(userId uint32, nodeId uint32) bool {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	// delete from GUT
	if userEntry, ok := r.gut[userId]; ok {
		if userEntry.NodeID == nodeId {
			delete(r.gut, userId)
			return true
		}
	}
	return false
}

func (r *AODVRouter) saveUsermessage(senderUserID, destUserId uint32, payload string) {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()

	umqe := userMessageQueueEntry{
		senderID: senderUserID,
		payload:  payload,
	}

	r.userMessageQueue[destUserId] = append(r.userMessageQueue[destUserId], umqe)
}

// getUserMessages returns all buffered messages for userId and clears them out.
func (r *AODVRouter) getUserMessages(userId uint32) []userMessageQueueEntry {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()

	msgs := r.userMessageQueue[userId]
	// clear the queue
	delete(r.userMessageQueue, userId)
	return msgs
}
