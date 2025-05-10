package sim

import (
	"encoding/json"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type NodeCfg struct {
	Count     int           `yaml:"count" json:"count"`
	Placement string        `yaml:"placement" json:"placement"` // uniform | grid | hotspot
	JoinDelay time.Duration `yaml:"join_delay" json:"join_delay"`
}

type UserCfg struct {
	PerNode int `yaml:"per_node" json:"per_node"`
}

type TrafficCfg struct {
	Pattern          string             `yaml:"pattern" json:"pattern"`
	MsgPerNodePerMin float64            `yaml:"msg_per_node_per_min" json:"msg_per_node_per_min"`
	PacketMix        map[string]float64 `yaml:"packet_mix" json:"packet_mix"`
}

type RoutingCfg struct {
	MaxHops int `yaml:"max_hops" json:"max_hops"`
}

type LogCfg struct {
	MetricsFile string `yaml:"metrics_file" json:"metrics_file"`
}

type Scenario struct {
	Duration     time.Duration `yaml:"duration" json:"duration"`
	Seed         int64         `yaml:"seed" json:"seed"`
	AreaKm2      float64       `yaml:"area_km2" json:"area_km2"`
	Nodes        NodeCfg       `yaml:"nodes" json:"nodes"`
	Users        UserCfg       `yaml:"users" json:"users"`
	StartupDelay time.Duration `yaml:"startup_delay" json:"startup_delay"`
	Traffic      TrafficCfg    `yaml:"traffic" json:"traffic"`
	Routing      RoutingCfg    `yaml:"routing" json:"routing"`
	Logging      LogCfg        `yaml:"logging" json:"logging"`
}

func LoadScenario(path string) (*Scenario, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc := &Scenario{}
	if yaml.Unmarshal(f, sc) == nil {
		return sc, nil
	}
	// fallback JSON
	if err := json.Unmarshal(f, sc); err != nil {
		return nil, err
	}
	return sc, nil
}
