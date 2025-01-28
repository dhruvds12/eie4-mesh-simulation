package main

import (
	"fmt"
	network "mesh-simulation/internal/network"
	node "mesh-simulation/internal/node"
	"time"
)

// ----------------------------------------------------------------------------
// Main Simulation
// ----------------------------------------------------------------------------

func main() {

	//Create the network and start it
	net := network.NewNetwork()
	go net.Run()

	//Create some initial nodes
	nodeA := node.NewNode("A")
	nodeB := node.NewNode("B")
	nodeC := node.NewNode("C")

	//Join the nodes to the network
	net.Join(nodeA)
	net.Join(nodeB)
	net.Join(nodeC)

	time.Sleep(1 * time.Second)

	// Peer to peer communication between nodes
	nodeA.SendData(net, "B", "SensorReading=123")
	nodeB.SendData(net, "C", "SensorReading=456")

	time.Sleep(2 * time.Second)
	// Create a new node and join it to the network
	nodeD := node.NewNode("D")
	net.Join(nodeD)

	time.Sleep(2 * time.Second)

	nodeD.SendData(net, "A", "Hello A, I'm new here!")

	time.Sleep(2 * time.Second)
	fmt.Println("Node B is leaving...")
	net.Leave("B")

	time.Sleep(2 * time.Second)
	// try send data to a node that is not in the network
	nodeD.SendData(net, "B", "Hello B, I'm new here!")

	time.Sleep(3 * time.Second)

	fmt.Println("Shutting down simulation.")

	net.Leave("A")
	net.Leave("C")
	net.Leave("D")

	time.Sleep(1 * time.Second)
}
