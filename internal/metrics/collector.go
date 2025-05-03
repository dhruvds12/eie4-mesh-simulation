package metrics

import (
	"encoding/json"
	"os"
	"sync"

	eb "mesh-simulation/internal/eventBus"
)

type Counters struct {
	TotalSent             uint64           `json:"total_sent"`
	TotalControlSent      uint64           `json:"total_control_sent"`
	TotalDelivered        uint64           `json:"total_delivered"`
	TotalControlDelivered uint64           `json:"total_control_delivered"`
	Collisions            uint64           `json:"collisions"`
	CollByType            map[uint8]uint64 `json:"collisions_by_type"`
	HopSum                uint64           `json:"hop_sum"`
	HopCount              uint64           `json:"hop_samples"`
}

type Collector struct {
	mu sync.Mutex
	Counters
}

func NewCollector() *Collector {
	return &Collector{Counters: Counters{CollByType: make(map[uint8]uint64)}}
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

func (c *Collector) AddSent() {
	c.mu.Lock()
	c.TotalSent++
	c.mu.Unlock()
}

func (c *Collector) AddControlSent() {
	c.mu.Lock()
	c.TotalControlSent++
	c.mu.Unlock()
}

func (c *Collector) AddDelivered(ev eb.Event) {
	c.mu.Lock()
	c.TotalDelivered++
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

func (c *Collector) Flush(file string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c.Counters)
}
