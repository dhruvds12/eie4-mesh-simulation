package mesh

type INode interface {
	GetID() uint32
	Run(net INetwork)
	SendData(net INetwork, destID uint32, payload string)
	BroadcastHello(net INetwork)
	HandleMessage(net INetwork, receivedPacket []byte)
	GetMessageChan() chan []byte
	GetQuitChan() chan struct{}
	PrintNodeDetails()

	GetPosition() Coordinates
	SetPosition(coord Coordinates)
}
