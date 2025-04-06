package message

import "github.com/google/uuid"

// TODO Change to be more like packet headers
// MessageType is a string representing different message categories.
type MessageType string

const (
	MsgHello    MessageType = "HELLO"
	MsgHelloAck MessageType = "HELLO_ACK"
	MsgData     MessageType = "DATA"
	DataAck     MessageType = "DATA_ACK"

	BroadcastID = "00000000-0000-0000-0000-000000000000"

	// Routing messages
	MsgRREQ MessageType = "RREQ"
	MsgRREP MessageType = "RREP"
	MsgRERR MessageType = "RERR"
)

// Message is a simple struct implementing IMessage.
type Message struct {
	Type    MessageType
	From    uuid.UUID
	Origin  uuid.UUID
	To      uuid.UUID
	Dest    uuid.UUID
	ID      string
	Payload string
}

// GetType returns the message type.
func (m *Message) GetType() MessageType {
	return m.Type
}

// GetFrom returns the sender's ID.
func (m *Message) GetFrom() uuid.UUID {
	return m.From
}

// GetTo returns the destination ID.
func (m *Message) GetTo() uuid.UUID {
	return m.To
}

// GetID returns the message ID.
func (m *Message) GetID() string {
	return m.ID
}

// GetPayload returns the payload string.
func (m *Message) GetPayload() string {
	return m.Payload
}

// GetDest returns the destination ID.
func (m *Message) GetDest() uuid.UUID {
	return m.Dest
}

// GetOrigin returns the origin ID.
func (m *Message) GetOrigin() uuid.UUID {
	return m.Origin
}
