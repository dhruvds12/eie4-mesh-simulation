package routing

import (
	"encoding/json"
	"log"
	"fmt"
	"time"

	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

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
}

// NewAODVRouter constructs a router for a specific node
func NewAODVRouter(ownerID uuid.UUID) *AODVRouter {
	return &AODVRouter{
		ownerID:    ownerID,
		routeTable: make(map[uuid.UUID]*RouteEntry),
		seenMsgIDs: make(map[string]bool),
	}
}

// -- IRouter methods --

func (r *AODVRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uuid.UUID, payload string) {
	// 1. Check if we already have a route
	entry, hasRoute := r.routeTable[destID]
	if !hasRoute {
		// Initiate RREQ
		log.Printf("Node %s (router) -> no route for %s, initiating RREQ.\n", r.ownerID, destID)
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
		log.Printf("Node %s (router) -> direct neighbor: %s\n", r.ownerID, neighborID)
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

    broadcastID := fmt.Sprintf("rreq-%s-%d", r.ownerID, time.Now().UnixNano())
    rreqMsg := &message.Message{
        Type:    message.MsgRREQ,
        From:    r.ownerID,
        To:      uuid.MustParse(message.BroadcastID),
        ID:      broadcastID, //TODO: this should be unique
        Payload: string(bytes),
    }
    net.BroadcastMessage(rreqMsg, sender)
}

func (r *AODVRouter) handleRREQ(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	if r.seenMsgIDs[msg.GetID()] {
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
		log.Printf("Node %s: RREQ arrived at destination.\n", r.ownerID)
		r.sendRREP(net, node, ctrl.Source, ctrl.Destination, 0)
		return
	}

	// If we have a route to the destination, we can send RREP
	if route, ok := r.routeTable[ctrl.Destination]; ok {
		r.sendRREP(net, node, ctrl.Source, ctrl.Destination, route.HopCount+1)
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
	net.UnicastMessage(fwdRrep, node)
}

// handleDataForward attempts to forward data if the node isn't the final dest
func (r *AODVRouter) handleDataForward(net mesh.INetwork, node mesh.INode, msg message.IMessage) {
	// If I'm the final destination, do nothing -> the node can "deliver" it
	if msg.GetDest() == r.ownerID {
		log.Printf("Node %s: DATA arrived. Payload = %q\n", r.ownerID, msg.GetPayload())
		return
	}

	// Otherwise, I should forward it if I have a route
	dest := msg.GetDest()
	route, ok := r.routeTable[dest]
	if !ok {
		// No route, we might trigger route discovery or drop
		log.Printf("Node %s: no route to forward DATA destined for %s.\n", r.ownerID, dest)
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
	net.UnicastMessage(fwdMsg, node)
}

// maybeAddRoute updates route if shorter
func (r *AODVRouter) maybeAddRoute(dest, nextHop uuid.UUID, hopCount int) {
	exist, ok := r.routeTable[dest]
	if !ok || hopCount < exist.HopCount {
		r.routeTable[dest] = &RouteEntry{
			Destination: dest,
			NextHop:     nextHop,
			HopCount:    hopCount,
		}
	}
}
