package message

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
	From    uint32
	Origin  uint32
	To      uint32
	Dest    uint32
	ID      string
	Payload string
}

// GetType returns the message type.
func (m *Message) GetType() MessageType {
	return m.Type
}

// GetFrom returns the sender's ID.
func (m *Message) GetFrom() uint32 {
	return m.From
}

// GetTo returns the destination ID.
func (m *Message) GetTo() uint32 {
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
func (m *Message) GetDest() uint32 {
	return m.Dest
}

// GetOrigin returns the origin ID.
func (m *Message) GetOrigin() uint32 {
	return m.Origin
}
