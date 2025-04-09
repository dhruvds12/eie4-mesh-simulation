package mqtt

import (
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTManager manages the MQTT connection and message routing.
type MQTTManager struct {
	client  mqtt.Client
	MsgChan chan mqtt.Message
}

// New creates and connects a new MQTTManager.
func New(broker, clientID string) *MQTTManager {
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID)
	manager := &MQTTManager{
		MsgChan: make(chan mqtt.Message, 100),
	}
	// Set a default handler to push messages onto the channel.
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		manager.MsgChan <- msg
	})

	manager.client = mqtt.NewClient(opts)
	if token := manager.client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	return manager
}

// Subscribe subscribes to a specific topic with the desired QoS.
func (m *MQTTManager) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) error {
	token := m.client.Subscribe(topic, qos, callback)
	token.Wait()
	return token.Error()
}

// Publish publishes a message to the given topic.
func (m *MQTTManager) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	token := m.client.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

// Run starts the processing loop for incoming MQTT messages.
func (m *MQTTManager) Run() {
	for msg := range m.MsgChan {
		fmt.Printf("Received message on topic %s: %s\n", msg.Topic(), msg.Payload())
		// Here, decode the message and dispatch it to the appropriate node handler
	}
}

// Disconnect performs a clean disconnect from the MQTT broker.
func (m *MQTTManager) Disconnect() {
	m.client.Disconnect(250)
	close(m.MsgChan)
}
