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
	Pattern               string             `yaml:"pattern" json:"pattern"`
	MsgPerNodePerMin      float64            `yaml:"msg_per_node_per_min" json:"msg_per_node_per_min"`
	RestrictToKnownRoutes bool               `yaml:"restrict_to_known_routes" json:"restrict_to_known_routes"`
	Acks                  float64            `yaml:"acks" json:"acks"`
	PacketMix             map[string]float64 `yaml:"packet_mix" json:"packet_mix"`
}

type RoutingCfg struct {
	RouterType         int `yaml:"router_type" json:"router_type"`
	MaxHops            int `yaml:"max_hops" json:"max_hops"`
	ReplyThresholdHops int `yaml:"reply_threshold_hops"  json:"reply_threshold_hops"`
	RREQHopLimit       int `yaml:"rreq_hop_limit"        json:"rreq_hop_limit"`
	UREQHopLimit       int `yaml:"ureq_hop_limit"        json:"ureq_hop_limit"`
}

type LogCfg struct {
	MetricsFile string `yaml:"metrics_file" json:"metrics_file"`
}

type CSMAcfg struct {
	CCAWindow      time.Duration `yaml:"cca_window" json:"cca_window"`
	CCASample      time.Duration `yaml:"cca_sample" json:"cca_sample"`
	InitialBackoff time.Duration `yaml:"initial_backoff" json:"initial_backoff"`
	MaxBackoff     time.Duration `yaml:"max_backoff" json:"max_backoff"`
	BackoffScheme  string        `yaml:"backoff_scheme" json:"backoff_scheme"` // binary | be
	BEUnit         time.Duration `yaml:"be_unit" json:"be_unit"`
	BEMaxExp       int           `yaml:"be_max_exp" json:"be_max_exp"`
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
	CSMA         CSMAcfg       `yaml:"csma"   json:"csma"`
	EndMode      string        `yaml:"end_mode" json:"end_mode"`
	DrainTimeout time.Duration `yaml:"drain_timeout" json:"drain_timeout"`
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

	// defaults:
	if sc.CSMA.CCAWindow == 0 {
		sc.CSMA.CCAWindow = 5 * time.Millisecond
	}
	if sc.CSMA.CCASample == 0 {
		sc.CSMA.CCASample = 100 * time.Microsecond
	}
	if sc.CSMA.InitialBackoff == 0 {
		sc.CSMA.InitialBackoff = 100 * time.Millisecond
	}
	if sc.CSMA.MaxBackoff == 0 {
		sc.CSMA.MaxBackoff = 2 * time.Second
	}
	if sc.CSMA.BackoffScheme == "" {
		sc.CSMA.BackoffScheme = "binary"
	}
	if sc.CSMA.BEUnit == 0 {
		sc.CSMA.BEUnit = 20 * time.Millisecond
	}
	if sc.CSMA.BEMaxExp == 0 {
		sc.CSMA.BEMaxExp = 5
	}
	if sc.EndMode == "" {
		sc.EndMode = "immediate"
	}

	if sc.Routing.MaxHops == 0 {
		sc.Routing.MaxHops = 10
	}
	if sc.Routing.ReplyThresholdHops == 0 {
		sc.Routing.ReplyThresholdHops = 2
	}
	if sc.Routing.RREQHopLimit == 0 {
		sc.Routing.RREQHopLimit = sc.Routing.MaxHops
	}
	if sc.Routing.UREQHopLimit == 0 {
		sc.Routing.UREQHopLimit = sc.Routing.MaxHops
	}

	return sc, nil
}
