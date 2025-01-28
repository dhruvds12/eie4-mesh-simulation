package mesh

import (
	"mesh-simulation/internal/message"

	"github.com/google/uuid"
)

type INode interface {
	GetID() uuid.UUID
	Run(net INetwork)
	SendData(net INetwork, destID uuid.UUID, payload string)
	BroadcastHello(net INetwork)
	HandleMessage(net INetwork, msg message.IMessage)
	GetMessageChan() chan message.IMessage
	GetQuitChan() chan struct{}
	PrintNodeDetails()

	GetPosition() Coordinates
	SetPosition(coord Coordinates)
}
