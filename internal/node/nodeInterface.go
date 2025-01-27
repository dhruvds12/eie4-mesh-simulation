package node

type INode interface {
	GetId() string
	GetPosition() (float64, float64)
	SendMessage(to INode, message string) error
	ReceiveMessage(message Message) error
	StartListening()
	ProcessMessage(msg Message)
	Broadcast(nodes []INode)
	GetRoutingTable() map[string]struct{}
}
