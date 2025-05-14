package routing

import (
	"mesh-simulation/internal/mesh"
)

// IRouter is the interface that all routing algorithms must implement.
type IRouter interface {
	// Called by the node to send data to destID
	SendData(net mesh.INetwork, sender mesh.INode, destID uint32, payload string)
	// Called by the node to send data to destID using CSMA
	// SendDataCSMA(net mesh.INetwork, sender mesh.INode, destID uint32, payload string)
	// Called by the node when it receives *any* message, so the router can process RREQ, RREP, or forward data
	HandleMessage(net mesh.INetwork, node mesh.INode, msg []byte)

	// The node notifies the router that "dest" is a direct neighbor
	AddDirectNeighbor(nodeID, neighborID uint32)

	// initial message to the network to broadcast a hello message
	SendBroadcastInfo(net mesh.INetwork, node mesh.INode)
	SendDiffBroadcastInfo(net mesh.INetwork, node mesh.INode);

	// print out the routing table
	PrintRoutingTable()

	// Start the router's Tx check go routine
	StartPendingTxChecker(net mesh.INetwork, node mesh.INode)

	// Stop the router's Tx check go routine
	StopPendingTxChecker()

	// Stop the router's Broadcast ticker go routine
	StopBroadcastTicker()

	StopTxWorker()

	// Add item to routing table
	AddRouteEntry(dest, nextHop uint32, hopCount int)

	// Remove item from routing table
	RemoveRouteEntry(dest uint32)

	BroadcastMessageCSMA(net mesh.INetwork, sender mesh.INode, sendPacket []byte, packetID uint32)
	SendUserMessage(net mesh.INetwork, sender mesh.INode, sendUserID, destUserID uint32, payload string)
}
