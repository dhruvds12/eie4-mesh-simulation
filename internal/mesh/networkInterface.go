package mesh

import (
	"mesh-simulation/internal/message"
)

type INetwork interface {
	Run()
	Join(n INode)
	Leave(nodeID string)
	BroadcastMessage(msg message.IMessage, sender INode)
	UnicastMessage(msg message.IMessage, sender INode)
}
