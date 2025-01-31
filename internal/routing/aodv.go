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

// AODVRouter is a per-node router
type AODVRouter struct {
	ownerID    uuid.UUID
	routeTable map[uuid.UUID]*RouteEntry // key = destination ID
	seenMsgIDs map[string]bool           // deduplicate RREQ/RREP
	dataQueue map[uuid.UUID][]string 	// queue of data to send after route is established
}

// NewAODVRouter constructs a router for a specific node
func NewAODVRouter(ownerID uuid.UUID) *AODVRouter {
	return &AODVRouter{
		ownerID:    ownerID,
		routeTable: make(map[uuid.UUID]*RouteEntry), // TODO: implement a timeout for routes
		seenMsgIDs: make(map[string]bool),
		dataQueue: make(map[uuid.UUID][]string),
	}
}

// -- IRouter methods --

func (r *AODVRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uuid.UUID, payload string) {
	// 1. Check if we already have a route
	entry, hasRoute := r.routeTable[destID]
	if !hasRoute {
		// Initiate RREQ
		log.Printf("[sim] Node %s (router) -> no route for %s, initiating RREQ.\n", r.ownerID, destID)
		r.dataQueue[destID] = append(r.dataQueue[destID], payload)
		r.initiateRREQ(net, sender, destID)
		return
	}

	// 2. We have a route -> unicast data to NextHop
	nextHop := entry.NextHop
	dataMsg := &message.Message{
		Type:    message.MsgData,
		From:    r.ownerID,
		To:      nextHop, //Todo: this could be nextHop
		Dest:    destID,
		ID:      fmt.Sprintf("data-%s-%s-%d", r.ownerID, destID, time.Now().UnixNano()),
		Payload: payload,
	}

	log.Printf("[sim] Node %s (router) -> forwarding data to %s via %s\n", r.ownerID, destID, nextHop)
	net.UnicastMessage(dataMsg, sender)
}

// HandleMessage is called when the node receives any message
func (r *AODVRouter) HandleMessage(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	switch msg.GetType() {
	case message.MsgRREQ:
		r.handleRREQ(net, node, msg)
	case message.MsgRREP:
		r.handleRREP(net, node, msg)
	case message.MsgData:
		r.handleDataForward(net, node, msg)
	default:
		// not a routing message
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
	net.UnicastMessage(rrepMsg, node)
}

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
	net.UnicastMessage(fwdRrep, node)
}

// handleDataForward attempts to forward data if the node isn't the final dest
func (r *AODVRouter) handleDataForward(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	// If I'm the final destination, do nothing -> the node can "deliver" it
	if msg.GetDest() == r.ownerID {
		log.Printf("[sim] Node %s: DATA arrived. Payload = %q\n", r.ownerID, msg.GetPayload())
		return
	}

	//TODO: Could this call SendData?

	// Otherwise, I should forward it if I have a route
	originID := msg.GetFrom()
	dest := msg.GetDest()
	route, ok := r.routeTable[dest]
	if !ok {
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %s: no route to forward DATA destined for %s.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		return
	}

	// Forward the data to NextHop
	fwdMsg := &message.Message{
		Type:    message.MsgData,
		From:    r.ownerID, // I'm forwarding
		To:      route.NextHop,
		Dest:    dest,
		ID:      msg.GetID(), // keep same ID or generate new
		Payload: msg.GetPayload(),
	}
	log.Printf("[sim] Node %s: forwarding DATA from %s to %s via %s\n", r.ownerID, originID, dest, route.NextHop)
	net.UnicastMessage(fwdMsg, node)
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
