package message

import "github.com/google/uuid"

type IMessage interface {
	GetType() MessageType
	GetFrom() uuid.UUID
	GetTo() uuid.UUID
	GetID() string
	GetPayload() string
	GetDest() uuid.UUID
}
