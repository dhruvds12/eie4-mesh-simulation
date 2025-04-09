package mqtt

// MqttNodePayload represents the JSON payload for registering or removing a node.
type MqttNodePayload struct {
	NodeID       string  `json:"node_id"`
	CommandTopic string  `json:"command_topic"`
	StatusTopic  string  `json:"status_topic"`
	Event        string  `json:"event"`
	Lat          float64 `json:"lat,omitempty"`  // optional coordinate
	Long         float64 `json:"long,omitempty"` // optional coordinate
}
