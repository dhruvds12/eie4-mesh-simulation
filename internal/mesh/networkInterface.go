package mesh

import (
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

type INetwork interface {
	Run()
	Join(n INode)
	Leave(nodeID uuid.UUID)
	BroadcastMessage(msg message.IMessage, sender INode)
	UnicastMessage(msg message.IMessage, sender INode)
	IsChannelFree(node INode) bool
	LeaveAll()
	GetNode(nodeId uuid.UUID) (INode, error)
}
