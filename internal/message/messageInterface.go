package message

type IMessage interface {
	GetType() MessageType
	GetFrom() uint32
	GetTo() uint32
	GetID() string
	GetPayload() string
	GetDest() uint32
	GetOrigin() uint32
}
