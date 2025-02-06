package routing

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

/*
TODO:
- Implement a timeout for routes
-Implement an ack for data packets (check what meshtastic does)
-Implement RERR (route error) messages when route is broken 
*/

// AODVControl is extra data for RREQ/RREP
type AODVControl struct {
	Source      uuid.UUID
	Destination uuid.UUID
	HopCount    int
}

// RouteEntry stores route info
type RouteEntry struct {
	Destination uuid.UUID
	NextHop     uuid.UUID
	HopCount    int
}

type RERRControl struct {
	BrokenNode uuid.UUID
	MessageDest 	 uuid.UUID
	MessageId string
	MessageSource uuid.UUID
}

// Used to store transactions that are pending ACKs
type PendingTx struct {
    MsgID       string
    Dest        uuid.UUID // destination of the data
    PotentialBrokenNode     uuid.UUID // (the broken node)
	Origin 	    uuid.UUID // original source of the data
    ExpiryTime  time.Time 
}

// AODVRouter is a per-node router
type AODVRouter struct {
	ownerID    uuid.UUID
	routeTable map[uuid.UUID]*RouteEntry // key = destination ID
	seenMsgIDs map[string]bool           // deduplicate RREQ/RREP
	dataQueue map[uuid.UUID][]string 	// queue of data to send after route is established
	pendingTxs map[string]PendingTx
	quitChan   chan struct{}
}

// NewAODVRouter constructs a router for a specific node
func NewAODVRouter(ownerID uuid.UUID) *AODVRouter {
	return &AODVRouter{
		ownerID:    ownerID,
		routeTable: make(map[uuid.UUID]*RouteEntry), // TODO: implement a timeout for routes 
		seenMsgIDs: make(map[string]bool),
		dataQueue: make(map[uuid.UUID][]string),
		pendingTxs: make(map[string]PendingTx),
		quitChan:   make(chan struct{}),
	}
}

// ------------------------------------------------------------
// runPendingTxChecker periodically checks pendingTxs for expired entries
// If expired, we assume we never overheard the forward => route is broken

func (r *AODVRouter) StartPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	go r.runPendingTxChecker(net, node)
}

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
                    log.Printf("[Timeout] Node %s: pendingTx expired for msgID=%s => route to %s is considered broken",
                        r.ownerID, msgID, tx.Dest)

                    // Remove from pendingTxs
                    delete(r.pendingTxs, msgID)

					// Invalidate routes
					r.InvalidateRoutes(tx.PotentialBrokenNode)

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

// Stop the pendingTxChecker
func (r *AODVRouter) StopPendingTxChecker() {
    close(r.quitChan)
}



// -- IRouter methods --

func (r *AODVRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uuid.UUID, payload string) {
	// Check if we already have a route
	entry, hasRoute := r.routeTable[destID]
	if !hasRoute {
		// Initiate RREQ
		log.Printf("[sim] Node %s (router) -> no route for %s, initiating RREQ.\n", r.ownerID, destID)
		r.dataQueue[destID] = append(r.dataQueue[destID], payload)
		r.initiateRREQ(net, sender, destID)
		return
	}

	// We have a route -> unicast data to NextHop
	msgID:= fmt.Sprintf("data-%s-%s-%d", r.ownerID, destID, time.Now().UnixNano())
	nextHop := entry.NextHop
	dataMsg := &message.Message{
		Type:    message.MsgData,
		From:    r.ownerID,
		Origin:  r.ownerID,
		To:      nextHop, //Todo: this could be nextHop
		Dest:    destID,
		ID:      msgID,
		Payload: payload,
	}

	log.Printf("[sim] Node %s (router) -> forwarding data to %s via %s\n", r.ownerID, destID, nextHop)
	net.BroadcastMessage(dataMsg, sender)

	expire := time.Now().Add(3 * time.Second) // e.g. 3s
	r.pendingTxs[msgID] = PendingTx{
		MsgID: msgID,
		Dest:  destID,
		PotentialBrokenNode: nextHop,
		Origin: r.ownerID,
		ExpiryTime: expire,
	}
}

// HandleMessage is called when the node receives any message
// TODO: repeated logic should be removed?????
func (r *AODVRouter) HandleMessage(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	switch msg.GetType() {
	case message.MsgRREQ:
		// All nodes who recieve should handle
		r.handleRREQ(net, node, msg)
	case message.MsgRREP:
		// Only the intended recipient should handle
		if msg.GetTo() == r.ownerID {
			r.handleRREP(net, node, msg)
		}
	case message.MsgRERR:
		// All nodes who recieve should handle
		r.handleRERR(net, node, msg)
	case message.MsgData:
		// Overhearing logic for implicit ACKs
		if pt, ok := r.pendingTxs[msg.GetID()]; ok && pt.PotentialBrokenNode == msg.GetFrom() {
            // ack
            log.Printf("{Implicit ACK}  Node %s: overheard forward from %s => implicit ack for msgID=%s", 
                       r.ownerID, msg.GetFrom(), msg.GetID())
            delete(r.pendingTxs, msg.GetID())
        }
		// Only the intended recipient should handle
		if msg.GetTo() == r.ownerID {
			r.handleDataForward(net, node, msg)
		}
	default:
		// not a routing message
		log.Printf("[sim] Node %s (router) -> received non-routing message: %s\n", r.ownerID, msg.GetType())
	}
}

// AddDirectNeighbor is called by the node when it discovers a new neighbor
func (r *AODVRouter) AddDirectNeighbor(nodeID, neighborID uuid.UUID) {
	// only do this if nodeID == r.ownerID
	if nodeID != r.ownerID {
		return
	}
	// If we don't have a route, or if this is a shorter route
	existing, exists := r.routeTable[neighborID]
	if !exists || (exists && existing.HopCount > 1) {
		r.routeTable[neighborID] = &RouteEntry{
			Destination: neighborID,
			NextHop:     neighborID,
			HopCount:    1,
		}
		log.Printf("[sim] [routing table] Node %s (router) -> direct neighbor: %s\n", r.ownerID, neighborID)
	}
}

func (r *AODVRouter) PrintRoutingTable() {
	fmt.Printf("Node %s (router) -> routing table:\n", r.ownerID)
	for dest, route := range r.routeTable {
		fmt.Printf("   %s -> via %s (hop count %d)\n", dest, route.NextHop, route.HopCount)
	}
}

// -- Private AODV logic --

func (r *AODVRouter) initiateRREQ(net mesh.INetwork, sender mesh.INode, destID uuid.UUID) {
	ctrl := AODVControl{
		Source:      r.ownerID,
		Destination: destID,
		HopCount:    0,
	}
	bytes, _ := json.Marshal(ctrl)

	// Create a unique broadcast ID
	broadcastID := fmt.Sprintf("rreq-%s-%d", r.ownerID, time.Now().UnixNano())
	// Save the broadcast ID in the saw list to avoid responding to own RREQ
	r.seenMsgIDs[broadcastID] = true
	rreqMsg := &message.Message{
		Type:    message.MsgRREQ,
		From:    r.ownerID,
		To:      uuid.MustParse(message.BroadcastID),
		ID:      broadcastID,
		Payload: string(bytes),
	}
	log.Printf("[sim] [RREQ init] Node %s (router) -> initiating RREQ for %s (hop count %d)\n", r.ownerID, destID, 0)
	net.BroadcastMessage(rreqMsg, sender)
}

// Every Node needs to handle this 
func (r *AODVRouter) handleRREQ(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	if r.seenMsgIDs[msg.GetID()] {
		log.Printf("Node %s: ignoring duplicate RREQ.\n", r.ownerID)
		return
	}
	r.seenMsgIDs[msg.GetID()] = true

	var ctrl AODVControl
	if err := json.Unmarshal([]byte(msg.GetPayload()), &ctrl); err != nil {
		return
	}

	// Add reverse route to RREQ source
	r.maybeAddRoute(ctrl.Source, msg.GetFrom(), ctrl.HopCount+1)

	// if I'm the destination, send RREP
	if r.ownerID == ctrl.Destination {
		log.Printf("[sim] Node %s: RREQ arrived at destination.\n", r.ownerID)
		r.sendRREP(net, node, ctrl.Source, ctrl.Destination, 0) // Should this reset to 0 (yes)
		return
	}

	// If we have a route to the destination, we can send RREP
	if route, ok := r.routeTable[ctrl.Destination]; ok {
		r.sendRREP(net, node, ctrl.Source, ctrl.Destination, route.HopCount)
		return
	}

	// Otherwise, rebroadcast RREQ
	ctrl.HopCount++
	newPayload, _ := json.Marshal(ctrl)
	fwdMsg := &message.Message{
		Type:    message.MsgRREQ,
		From:    r.ownerID,
		To:      msg.GetTo(), // broadcast
		ID:      msg.GetID(),
		Payload: string(newPayload),
	}
	log.Printf("[sim] [RREQ FORWARD] Node %s: forwarding RREQ for %s (hop count %d)\n", r.ownerID, ctrl.Destination, ctrl.HopCount)
	net.BroadcastMessage(fwdMsg, node)
}

func (r *AODVRouter) sendRREP(net mesh.INetwork, node mesh.INode, source, destination uuid.UUID, hopCount int) {
	// find route to 'source' in reverse direction
	reverseRoute := r.routeTable[source]
	if reverseRoute == nil {
		log.Printf("Node %s: can't send RREP, no route to source %s.\n", r.ownerID, source)
		return
	}

	rrepData := AODVControl{
		Source:      destination,
		Destination: source,
		HopCount:    hopCount,
	}
	bytes, _ := json.Marshal(rrepData)
	rrepMsg := &message.Message{
		Type:    message.MsgRREP,
		From:    r.ownerID,
		To:      reverseRoute.NextHop,
		ID:      fmt.Sprintf("rrep-%s-%s-%d", destination, source, time.Now().UnixNano()),
		Payload: string(bytes),
	}
	log.Printf("[sim] [RREP] Node %s: sending RREP to %s via %s current hop count: %d\n", r.ownerID, source, reverseRoute.NextHop, hopCount)
	net.BroadcastMessage(rrepMsg, node)
}

// Only node specified in the RREP message should handle this
func (r *AODVRouter) handleRREP(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	if r.seenMsgIDs[msg.GetID()] {
		return
	}
	r.seenMsgIDs[msg.GetID()] = true

	var ctrl AODVControl
	if err := json.Unmarshal([]byte(msg.GetPayload()), &ctrl); err != nil {
		return
	}

	// Add forward route to ctrl.Source
	r.maybeAddRoute(ctrl.Source, msg.GetFrom(), ctrl.HopCount+1)

	// if I'm the original route requester, done
	if r.ownerID == ctrl.Destination {
		log.Printf("Node %s: route to %s established!\n", r.ownerID, ctrl.Source)
		// send any queued data
		for _, payload := range r.dataQueue[ctrl.Source] {
			r.SendData(net, node, ctrl.Source, payload)
		}
		delete(r.dataQueue, ctrl.Source)
		return
	}

	// else forward RREP
	reverseRoute := r.routeTable[ctrl.Destination]
	if reverseRoute == nil {
		log.Printf("Node %s: got RREP but no route back to %s.\n", r.ownerID, ctrl.Destination)
		return
	}

	ctrl.HopCount++
	bytes, _ := json.Marshal(ctrl)
	fwdRrep := &message.Message{
		Type:    message.MsgRREP,
		From:    r.ownerID,
		To:      reverseRoute.NextHop,
		ID:      msg.GetID(),
		Payload: string(bytes),
	}
	log.Printf("[RREP FORWARD] Node %s: forwarding RREP to %s via %s\n", r.ownerID, ctrl.Destination, reverseRoute.NextHop)
	net.BroadcastMessage(fwdRrep, node)
}

func (r *AODVRouter) sendRERR(net mesh.INetwork, node mesh.INode, to uuid.UUID, dataDest uuid.UUID, brokenNode uuid.UUID, messageId string, messageSource uuid.UUID) {
    rerr := RERRControl{
        BrokenNode: brokenNode,
        MessageDest:       dataDest,
		MessageId: messageId,
		MessageSource: messageSource,
    }
    j, _ := json.Marshal(rerr)
    msgID := fmt.Sprintf("rerr-%s-%d", r.ownerID, time.Now().UnixNano())
    r.seenMsgIDs[msgID] = true
    rerrMsg := &message.Message{
        Type:    message.MsgRERR,
        From:    r.ownerID,
        To:      to, // unicast to upstream node
        ID:      msgID,
        Payload: string(j),
    }
    log.Printf("[sim] Node %s: sending RERR to %s about broken route for %s.\n", r.ownerID, to, dataDest)
    net.BroadcastMessage(rerrMsg, node)
}

// Everyone who receives RERR should handle this (only intended recipient should forward to source) (source should not forward)
func (r *AODVRouter) handleRERR(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
    if r.seenMsgIDs[msg.GetID()] {
        return
    }
    r.seenMsgIDs[msg.GetID()] = true

    var rc RERRControl
    if err := json.Unmarshal([]byte(msg.GetPayload()), &rc); err != nil {
        return
    }

    log.Printf("[sim] Node %s: received RERR => broken node: %s for dest %s\n", r.ownerID, rc.BrokenNode, rc.MessageDest)

	// Invalidate routes
	r.InvalidateRoutes(rc.BrokenNode)

	if r.ownerID != msg.GetTo() {
		// Check if node is the intended target
		log.Printf("{RERR} Node %s: received RERR not intended for me.\n", r.ownerID)
		return
	}

	if r.ownerID == rc.MessageSource {
		log.Printf("{RERR} Node %s: received RERR for my own message, stopping here.\n", r.ownerID)
		return
	}


	entry, hasRoute := r.routeTable[rc.MessageSource]
	if !hasRoute {
		// No route to source, can't forward RERR 
		log.Printf("[sim] {RERR FAIL} Node %s: no route to forward RERR destined for %s.\n", r.ownerID, rc.MessageSource)
		// TODO: might need to initiate RREQ to source
		return
	}

	// If we have a route to the source of the message, we can forward the RERR
	nexthop := entry.NextHop
	r.sendRERR(net, node, nexthop, rc.MessageDest, rc.BrokenNode, rc.MessageId, rc.MessageSource)

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
func (r *AODVRouter) handleDataForward(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	// If I'm the final destination, do nothing -> the node can "deliver" it
	if msg.GetDest() == r.ownerID {
		log.Printf("[sim] Node %s: DATA arrived. Payload = %q\n", r.ownerID, msg.GetPayload())
		// Send an ACK back to the sender
		// r.sendDataAck(net, node, msg.GetFrom())
		return
	}

	//TODO: Could this call SendData?

	// Otherwise, I should forward it if I have a route
	
	dest := msg.GetDest()
	route, ok := r.routeTable[dest]
	if !ok {
		// This is in theory an unlikely case because the origin node should have initiated RREQ
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %s: no route to forward DATA destined for %s.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		// no route therefore, we need ot send RERR 
		r.sendRERR(net, node, msg.GetFrom(), dest, r.ownerID, msg.GetID(), msg.GetOrigin())
		return
	}

	// Forward the data to NextHop
	fwdMsg := &message.Message{
		Type:    message.MsgData,
		From:    r.ownerID, // I'm forwarding
		To:      route.NextHop,
		Origin:  msg.GetOrigin(),
		Dest:    dest,
		ID:      msg.GetID(), // keep same ID or generate new
		Payload: msg.GetPayload(),
	}
	log.Printf("[sim] Node %s: forwarding DATA from %s to %s via %s\n", r.ownerID, msg.GetFrom(), dest, route.NextHop)
	net.BroadcastMessage(fwdMsg, node)

	// Implicit ACK: if the next hop is the intended recipient, we can assume the data was received
	if route.NextHop == dest {
		// log.Printf("{Implicit ACK} Node %s: overheard forward from %s => implicit ack for msgID=%s", r.ownerID, originID, msg.GetID())
		// TODO: need to wait for an explicit ACK in the future
		return
	}

	// If the next hop is not the destination, we need to track the transaction by overhearing it
	expire := time.Now().Add(3 * time.Second) // e.g. 3s
	r.pendingTxs[msg.GetID()] = PendingTx{
		MsgID: msg.GetID(),
		Dest:  dest,
		PotentialBrokenNode: route.NextHop,
		Origin: msg.GetOrigin(),
		ExpiryTime: expire,
	}

}

func (r *AODVRouter) InvalidateRoutes(brokenNode uuid.UUID) {
    // Remove any direct route to the broken node.
    if _, ok := r.routeTable[brokenNode]; ok {
        log.Printf("Node %s: removing direct route to broken node %s.\n", r.ownerID, brokenNode)
        delete(r.routeTable, brokenNode)
    }
    // Iterate through the routing table.
    for dest, route := range r.routeTable {
        if route.NextHop == brokenNode {
            log.Printf("Node %s: invalidating route to %s because NextHop %s is broken.\n", r.ownerID, dest, brokenNode)
            delete(r.routeTable, dest)
        }
    }
}

// send ack for data packets
func (r *AODVRouter) sendDataAck(net mesh.INetwork, node mesh.INode, dest uuid.UUID) {
	ackMsg := &message.Message{
		Type:    message.DataAck,
		From:    r.ownerID,
		To:      dest,
		ID:      fmt.Sprintf("ack-%s-%s-%d", r.ownerID, dest, time.Now().UnixNano()),
		Payload: "ACK",
	}
	log.Printf("[sim] Node %s: sending DATA_ACK to %s\n", r.ownerID, dest)
	net.BroadcastMessage(ackMsg, node)
}

// maybeAddRoute updates route if shorter
func (r *AODVRouter) maybeAddRoute(dest, nextHop uuid.UUID, hopCount int) {
	exist, ok := r.routeTable[dest]
	if !ok || hopCount < exist.HopCount {
		// log an update to the routing table
		log.Printf("[sim] [routing table] Node %s (router) -> updated route to %s via %s (hop count %d)\n", r.ownerID, dest, nextHop, hopCount)
		r.routeTable[dest] = &RouteEntry{
			Destination: dest,
			NextHop:     nextHop,
			HopCount:    hopCount,
		}
	}
}
