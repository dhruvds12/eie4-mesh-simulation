package metrics

import (
	"encoding/json"
	"os"
	"sync"

	eb "mesh-simulation/internal/eventBus"
)

type Counters struct {
	TotalSent              uint64           `json:"total_sent"`
	TotalSentByType        map[uint8]uint64 `json:"sent_by_type"`
	TotalControlSent       uint64           `json:"total_control_sent"`
	TotalControlSentByType map[uint8]uint64 `json:"control_sent_by_type"`
	TotalDelivered         uint64           `json:"total_delivered"`
	TotalDeliveredByType   map[uint8]uint64 `json:"delivered_by_type"`
	TotalControlDelivered  uint64           `json:"total_control_delivered"`
	Collisions             uint64           `json:"collisions"`
	CollByType             map[uint8]uint64 `json:"collisions_by_type"`
	HopSum                 uint64           `json:"hop_sum"`
	HopCount               uint64           `json:"hop_samples"`
	LostMessages           uint64           `json:"total_lost"`
	NoRoute                uint64           `json:"no_route"`
	NoRouteUser            uint64           `json:"no_route_user"`
	UserNotAtNode          uint64           `json:"user_not_at_node"`
	ReceivedAck            uint64           `json:"received_ack"`
	RequestedAck           uint64           `json:"requested_ack"`
	TxQueueDrop            uint64           `json:"tx_queue_drop"`
}

type Collector struct {
	mu sync.Mutex
	Counters
}

func NewCollector() *Collector {
	return &Collector{Counters: Counters{TotalSentByType: make(map[uint8]uint64), TotalDeliveredByType: make(map[uint8]uint64), TotalControlSentByType: make(map[uint8]uint64), CollByType: make(map[uint8]uint64)}}
}

func (c *Collector) AddCollision(pktType uint8) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Collisions++
	c.CollByType[pktType]++
}

func (c *Collector) AddSent(ev eb.Event) {
	c.mu.Lock()
	c.TotalSent++
	c.TotalSentByType[ev.PacketType]++
	c.mu.Unlock()
}

func (c *Collector) AddControlSent(ev eb.Event) {
	c.mu.Lock()
	c.TotalControlSent++
	c.TotalControlSentByType[ev.PacketType]++
	c.mu.Unlock()
}

func (c *Collector) AddDelivered(ev eb.Event) {
	c.mu.Lock()
	c.TotalDelivered++
	c.TotalDeliveredByType[ev.PacketType]++
	if ev.OtherNodeID != 0 {
		c.HopSum += uint64(ev.OtherNodeID)
		c.HopCount++
	}
	c.mu.Unlock()
}

func (c *Collector) AddControlDelivered(ev eb.Event) {
	c.mu.Lock()
	c.TotalControlDelivered++
	// if ev.OtherNodeID != 0 {
	//     c.HopSum += uint64(ev.OtherNodeID)
	//     c.HopCount++
	// }
	c.mu.Unlock()
}

func (c *Collector) AddLostMessage() {
	c.mu.Lock()
	c.LostMessages++
	c.mu.Unlock()
}

func (c *Collector) AddNoRoute() {
	c.mu.Lock()
	c.NoRoute++
	c.mu.Unlock()
}

func (c *Collector) AddNoRouteUser() {
	c.mu.Lock()
	c.NoRouteUser++
	c.mu.Unlock()
}

func (c *Collector) AddUserNotAtNode() {
	c.mu.Lock()
	c.UserNotAtNode++
	c.mu.Unlock()
}

func (c *Collector) AddReceivedDataAck() {
	c.mu.Lock()
	c.ReceivedAck++
	c.mu.Unlock()
}

func (c *Collector) AddRequestedACK() {
	c.mu.Lock()
	c.RequestedAck++
	c.mu.Unlock()
}

func (c *Collector) AddTxQueueDrop() {
	c.mu.Lock()
	c.TxQueueDrop++
	c.mu.Unlock()
}

func (c *Collector) Flush(file string) error {
	c.mu.Lock()
	snap := Counters{
		TotalSent:              c.TotalSent,
		TotalControlSent:       c.TotalControlSent,
		TotalDelivered:         c.TotalDelivered,
		TotalControlDelivered:  c.TotalControlDelivered,
		Collisions:             c.Collisions,
		HopSum:                 c.HopSum,
		HopCount:               c.HopCount,
		LostMessages:           c.LostMessages,
		NoRoute:                c.NoRoute,
		NoRouteUser:            c.NoRouteUser,
		UserNotAtNode:          c.UserNotAtNode,
		ReceivedAck:            c.ReceivedAck,
		RequestedAck:           c.RequestedAck,
		TxQueueDrop:            c.TxQueueDrop,
		TotalSentByType:        make(map[uint8]uint64, len(c.TotalSentByType)),
		TotalControlSentByType: make(map[uint8]uint64, len(c.TotalControlSentByType)),
		TotalDeliveredByType:   make(map[uint8]uint64, len(c.TotalDeliveredByType)),
		CollByType:             make(map[uint8]uint64, len(c.CollByType)),
	}
	for k, v := range c.TotalSentByType {
		snap.TotalSentByType[k] = v
	}
	for k, v := range c.TotalControlSentByType {
		snap.TotalControlSentByType[k] = v
	}
	for k, v := range c.TotalDeliveredByType {
		snap.TotalDeliveredByType[k] = v
	}
	for k, v := range c.CollByType {
		snap.CollByType[k] = v
	}
	c.mu.Unlock()

	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}
