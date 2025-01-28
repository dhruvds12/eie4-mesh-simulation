package message

// TODO Change to be more like packet headers
// MessageType is a string representing different message categories.
type MessageType string

const (
	MsgHello    MessageType = "HELLO"
	MsgHelloAck MessageType = "HELLO_ACK"
	MsgData     MessageType = "DATA"

	BroadcastID = "BROADCAST"
)

// Message is a simple struct implementing IMessage.
type Message struct {
	Type    MessageType
	From    string
	To      string
	ID      string
	Payload string
}

// GetType returns the message type.
func (m *Message) GetType() MessageType {
	return m.Type
}

// GetFrom returns the sender's ID.
func (m *Message) GetFrom() string {
	return m.From
}

// GetTo returns the destination ID.
func (m *Message) GetTo() string {
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
