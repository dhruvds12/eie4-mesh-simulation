package eventBus

import (
	"log"
	"sync"
	"time"
)

type EventType string

const (
	EventNodeJoined              EventType = "NODE_JOINED"
	EventNodeLeft                EventType = "NODE_LEFT"
	EventMessageSent             EventType = "MESSAGE_SENT"
	EventMessageDelivered        EventType = "MESSAGE_DELIVERED"
	EventAddRouteEntry           EventType = "ADD_ROUTE_ENTRY"
	EventMovedNode               EventType = "MOVED_NODE"
	EventRemoveRouteEntry        EventType = "REMOVED_ROUTE_ENTRY"
	EventCreateUser              EventType = "CREATE_USER"
	EventDeleteUser              EventType = "DELETE_USER"
	EventUserMessage             EventType = "USER_MESSAGE"
	EventControlMessageDelivered EventType = "CONTROL_MESSAGE_DELIVERED"
	EventControlMessageSent      EventType = "CONTROL_MESSAGE_SENT"
	EventLostMessage             EventType = "LOST_MESSAGE"
	EventNoRoute                 EventType = "NO_ROUTE"
	EventNoRouteUser             EventType = "NO_ROUTE_USER"
	EventUserNotAtNode           EventType = "USER_NOT_ATNODE"
	EventRequestedACK            EventType = "REQUESTED_ACK"
	EventReceivedDataAck         EventType = "RECEIVED_ACK"
	EventTxQueueDrop             EventType = "TX_QUEUE_DROP"
)

// RouteEntry represents an entry in the routing table.
type RouteEntry struct {
	Destination uint32
	NextHop     uint32
	HopCount    int
}

// Event holds details that the front end might need.
type Event struct {
	Type              EventType  `json:"type"`
	NodeID            uint32     `json:"node_id"`
	OtherNodeID       uint32     `json:"other_node_id"`
	MessageID         uint32     `json:"message_id"`
	RoutingTableEntry RouteEntry `json:"routing_table,omitempty"`
	Payload           string     `json:"payload,omitempty"`
	Timestamp         time.Time  `json:"timestamp"`
	X                 float64    `json:"x"`
	Y                 float64    `json:"y"`
	Virtual           bool       `json:"virtual"`
	UserID            uint32     `json:"user_id"` // also the send user
	DestUserID        uint32     `json:"dest_user_id"`
	PacketType        uint8      `json:"packet_type"`
}

// EventBus manages a set of subscribers and publishes events to them.
type EventBus struct {
	subscribers []chan Event
	mu          sync.RWMutex
}

// NewEventBus creates a new EventBus instance.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make([]chan Event, 0),
	}
}

// Publish sends an event to all subscribers.
func (eb *EventBus) Publish(e Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, sub := range eb.subscribers {
		// Use a non-blocking send in case a subscriber is busy.
		select {
		case sub <- e:
		default:
			log.Println("Dropping event: subscriber channel is full")
		}
	}
}

// Subscribe returns a new channel that will receive published events.
func (eb *EventBus) Subscribe() chan Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	ch := make(chan Event, 100) // adjust buffer size as needed
	eb.subscribers = append(eb.subscribers, ch)
	return ch
}
