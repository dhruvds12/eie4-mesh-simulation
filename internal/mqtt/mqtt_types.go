package mqtt

// MqttNodePayload represents the JSON payload for registering or removing a node.
type MqttNodePayload struct {
	NodeID       uint32  `json:"node_id"`
	CommandTopic string  `json:"command_topic"`
	ProcessTopic string  `json:"process_topic"`
	SendTopic    string  `json:"send_topic"`
	Event        string  `json:"event"`
	Lat          float64 `json:"lat,omitempty"`  // optional coordinate
	Long         float64 `json:"long,omitempty"` // optional coordinate
}
