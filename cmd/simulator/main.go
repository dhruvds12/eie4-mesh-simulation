package main

import (
	"io"
	"log"
	"os"
	"time"

	network "mesh-simulation/internal/network"
	node "mesh-simulation/internal/node"
)

// ----------------------------------------------------------------------------
// Main Simulation
// ----------------------------------------------------------------------------
func main() {
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll("logs", 0755); err != nil {
		log.Fatalf("Failed to create logs directory: %v", err)
	}

	// Create log file with timestamp in name
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	logFile, err := os.OpenFile("logs/log_"+timestamp+".log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Create a MultiWriter to write to both the log file and stdout
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	// Set log output to multiWriter
	log.SetOutput(multiWriter)

	// log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetFlags(0)

	// Log start of simulation
	log.Println("Starting simulation...")

	simulationV4()

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
	log.Printf("%s is leaving...", nodeB.GetID())
	log.Println()
	net.Leave(nodeB.GetID())

	time.Sleep(2 * time.Second)
	// try send data to a node that is not in the network
	nodeD.SendData(net, nodeB.GetID(), "Hello B, I'm new here!")

	time.Sleep(3 * time.Second)

	log.Println("Shutting down simulation.")

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
	nodeD := node.NewNode(0, 3000)

	netw.Join(nodeA)
	netw.Join(nodeB)
	netw.Join(nodeC)
	netw.Join(nodeD)

	// Sleep so they can broadcast HELLO and find each other
	time.Sleep(2 * time.Second)

	log.Println()
	log.Println()
	log.Println()
	log.Println()

	// nodeB.SendData(netw, nodeD.GetID(), "Hello from B to D")
	// time.Sleep(10 * time.Second)

	log.Println()
	log.Println()
	log.Println()
	log.Println()
	// Now let's test multi-hop. A wants to send data to C
	log.Println("NodeA -> NodeD: sending data! -> setting up route")
	nodeA.SendData(netw, nodeD.GetID(), "Hello from A to D")

	time.Sleep(10 * time.Second)
	log.Println()
	log.Println()

	netw.Leave(nodeD.GetID())
	time.Sleep(5 * time.Second)
	log.Println()
	log.Println()

	// Have to send message twice as first time is used to discover route -> to be fixed
	// log.Println("NodeA -> NodeD: sending data!")
	nodeA.SendData(netw, nodeD.GetID(), "Hello from A to D")

	// Let the route discovery / data forwarding happen
	time.Sleep(20 * time.Second)

	log.Println("Shutting down.")
	netw.Leave(nodeA.GetID())
	netw.Leave(nodeB.GetID())
	netw.Leave(nodeC.GetID())
	// netw.Leave(nodeD.GetID())
	time.Sleep(1 * time.Second)

	// TODO: This test case pases because the final node is not expected to ack the message -> to be fixed
}

func simulationV3() {
	netw := network.NewNetwork()
	go netw.Run()

	nodeA := node.NewNode(0, 0)
	nodeC := node.NewNode(0, 2000)
	nodeD := node.NewNode(0, 3000)
	nodeB := node.NewNode(0, 1000)

	netw.Join(nodeA)
	netw.Join(nodeB)
	netw.Join(nodeC)
	netw.Join(nodeD)

	// Sleep so they can broadcast HELLO and find each other	
	time.Sleep(5 * time.Second)
	
	log.Println()
	log.Println("Node A ID is: ", nodeA.GetID())
	log.Println()
	
	
	
	nodeD.SendData(netw, nodeA.GetID(), "Hello from D to A")
	
	time.Sleep(10 * time.Second)
	
	netw.Leave(nodeA.GetID())
	netw.Leave(nodeB.GetID())
	netw.Leave(nodeC.GetID())
	netw.Leave(nodeD.GetID())
	time.Sleep(1 * time.Second)
}


func simulationV4() {
	netw := network.NewNetwork()
	go netw.Run()

	nodeA := node.NewNode(0, 0)
	nodeB := node.NewNode(0, 1000)
	nodeC := node.NewNode(0, 2000)
	nodeD := node.NewNode(0, 3000)
	nodeE := node.NewNode(0, -1000)

	netw.Join(nodeA)
	netw.Join(nodeB)
	netw.Join(nodeC)
	netw.Join(nodeD)
	netw.Join(nodeE)

	// Sleep so they can broadcast HELLO and find each other	
	time.Sleep(5 * time.Second)
	
	log.Println()
	// log.Println("Node A ID is: ", nodeA.GetID())

	log.Println()
	
	
	
	nodeA.SendData(netw, nodeE.GetID(), "Hello from A to E")
	nodeC.SendData(netw, nodeB.GetID(), "Hello from C to B")
	
	time.Sleep(20 * time.Second)
	
	netw.Leave(nodeA.GetID())
	netw.Leave(nodeB.GetID())
	netw.Leave(nodeC.GetID())
	netw.Leave(nodeD.GetID())
	time.Sleep(1 * time.Second)
}