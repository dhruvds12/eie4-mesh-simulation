package mesh

import (
	"mesh-simulation/internal/message"
)

type INode interface {
	GetID() string
	Run(net INetwork)

	// Example actions:
	SendData(net INetwork, destID, payload string)
	BroadcastHello(net INetwork)
	HandleMessage(net INetwork, msg message.IMessage)
	GetMessageChan() chan message.IMessage
	GetQuitChan() chan struct{}
}
