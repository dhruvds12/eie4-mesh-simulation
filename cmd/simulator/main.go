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
	simulationV2()

}

func simulationV1() {
	//Create the network and start it
	net := network.NewNetwork()
	go net.Run()

	//Create some initial nodes
	// TODO: Needs to change to actual coordinates - this is just for testing
	// TODO: Hopefully setting them as actual coordinates will make it easy to overlay on a map
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

func simulationV2() {
	netw := network.NewNetwork()
	go netw.Run()


	nodeA := node.NewNode(0, 0)    
	nodeB := node.NewNode(0, 1000) 
	nodeC := node.NewNode(0, 2000) 

	netw.Join(nodeA)
	netw.Join(nodeB)
	netw.Join(nodeC)

	// Sleep so they can broadcast HELLO and find each other
	time.Sleep(2 * time.Second)

	// Now let's test multi-hop. A wants to send data to C
	fmt.Println("NodeA -> NodeC: sending data! -> setting up route")
	nodeA.SendData(netw, nodeC.GetID(), "Hello from A to C")

	time.Sleep(30 * time.Second)
	
	// Have to send message twice as first time is used to discover route -> to be fixed
	fmt.Println("NodeA -> NodeC: sending data!")
	fmt.Println("NodeA iD: ", nodeA.GetID())
	fmt.Println("NodeC iD: ", nodeC.GetID())
	nodeA.SendData(netw, nodeC.GetID(), "Hello from A to C")

	// Let the route discovery / data forwarding happen
	time.Sleep(30 * time.Second)

	fmt.Println("Shutting down.")
	netw.Leave(nodeA.GetID())
	netw.Leave(nodeB.GetID())
	netw.Leave(nodeC.GetID())
	time.Sleep(1 * time.Second)
}
