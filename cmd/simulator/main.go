package main

import (
	"fmt"
	"time"

	network "mesh-simulation/internal/network"
	node "mesh-simulation/internal/node"
)

// ----------------------------------------------------------------------------
// Main Simulation
// ----------------------------------------------------------------------------

func main() {

	//Create the network and start it
	net := network.NewNetwork()
	go net.Run()

	//Create some initial nodes
	nodeA := node.NewNode(0, 0)
	nodeB := node.NewNode(2800, 0)
	nodeC := node.NewNode(1400, 0)

	//Join the nodes to the network
	net.Join(nodeA)
	net.Join(nodeB)
	net.Join(nodeC)

	time.Sleep(1 * time.Second)

	// Peer to peer communication between nodes
	nodeA.SendData(net, nodeB.GetID(), "SensorReading=123")
	nodeB.SendData(net, nodeC.GetID(), "SensorReading=456")

	time.Sleep(2 * time.Second)
	// Create a new node and join it to the network
	nodeD := node.NewNode(700, 0)
	net.Join(nodeD)

	time.Sleep(2 * time.Second)

	nodeD.SendData(net, nodeA.GetID(), "Hello A, I'm new here!")

	time.Sleep(2 * time.Second)
	fmt.Printf("%s is leaving...", nodeB.GetID())
	fmt.Println()
	net.Leave(nodeB.GetID())

	time.Sleep(2 * time.Second)
	// try send data to a node that is not in the network
	nodeD.SendData(net, nodeB.GetID(), "Hello B, I'm new here!")

	time.Sleep(3 * time.Second)

	fmt.Println("Shutting down simulation.")

	net.Leave(nodeA.GetID())
	net.Leave(nodeC.GetID())
	net.Leave(nodeD.GetID())

	time.Sleep(1 * time.Second)
}
