package message

type IMessage interface {
	GetType() MessageType
	GetFrom() string
	GetTo() string
	GetID() string
	GetPayload() string
}
