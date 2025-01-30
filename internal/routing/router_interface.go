package routing

import (
    "github.com/google/uuid"
    "mesh-simulation/internal/message"
    "mesh-simulation/internal/mesh"
)

// IRouter is the interface that all routing algorithms must implement.
type IRouter interface {
    // Called by the node to send data to destID
    SendData(net mesh.INetwork, sender mesh.INode, destID uuid.UUID, payload string)
    // Called by the node when it receives *any* message, so the router can process RREQ, RREP, or forward data
    HandleMessage(net mesh.INetwork, node mesh.INode, msg message.IMessage)

    // The node notifies the router that "dest" is a direct neighbor
    AddDirectNeighbor(nodeID, neighborID uuid.UUID)

    // print out the routing table
    PrintRoutingTable()
}
