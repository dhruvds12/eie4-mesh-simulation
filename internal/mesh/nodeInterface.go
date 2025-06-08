package mesh

import (
	"time"
)

type INode interface {
	GetID() uint32
	Run(net INetwork)
	SendData(net INetwork, destID uint32, payload string, flags uint8)
	SendBroadcastInfo(net INetwork)
	HandleMessage(net INetwork, receivedPacket []byte)
	GetMessageChan() chan []byte
	GetQuitChan() chan struct{}
	PrintNodeDetails()

	GetPosition() Coordinates
	SetPosition(coord Coordinates)

	IsVirtual() bool

	GetConnectedUsers() []uint32
	HasConnectedUser(userID uint32) bool

	AddConnectedUser(userID uint32)
	RemoveConnectedUser(userID uint32)

	SendUserMessage(net INetwork, userID, destUserID uint32, payload string, flags uint8)

	SetRouterConstants(CCAWindow, CCASample, InitialBackoff, MaxBackoff time.Duration, BackoffScheme string, BEUnit time.Duration, BEMaxExp int) bool

	GetRandomKnownNode() (uint32, bool)

	SetRoutingParams(th, rreqLim, ureqLim int) bool

	GetRandomKnownUser() (userID uint32, ok bool)
}
