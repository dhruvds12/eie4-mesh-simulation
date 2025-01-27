package simulation

import (
	"fmt"
	"time"

	"github.com/dhruvds12/eie4-mesh-simulation/internal/node"
	"github.com/dhruvds12/eie4-mesh-simulation/internal/utils"
)

// RunSimulation initializes the network and starts the simulation
func RunSimulation(numNodes int, duration time.Duration) {
	nodes := []node.INode{} // Use interface type
	utils.MonitorResources(1 * time.Second)

	// Create and start nodes
	for i := 0; i < numNodes; i++ {
		n := node.NewNode(float64(i), float64(i)) // Pass position or randomize
		n.StartListening()
		nodes = append(nodes, n)
	}

	fmt.Println("Simulation started with", numNodes, "nodes.")

	// Graceful stop using a done channel
	done := make(chan bool)

	// Simulate periodic broadcasts
	go func() {
		// Initial broadcast
		for _, n := range nodes {
			n.Broadcast(nodes)
		}

		// Broadcast every 2 minutes
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				for _, n := range nodes {
					n.Broadcast(nodes)
				}
			case <-done:
				return
			}
		}
	}()

	// Run simulation for specified duration
	time.Sleep(duration)
	close(done) // Stop the broadcasting goroutine

	// Print final state of all nodes
	printNodeStates(nodes)

	fmt.Println("Simulation ended.")
}

// printNodeStates prints the final state of each node
func printNodeStates(nodes []node.INode) {
	fmt.Println("\nFinal Node States:")
	for _, n := range nodes {
		x, y := n.GetPosition() // Extract both values
		fmt.Printf("Node %s:\n", n.GetId())
		fmt.Printf("  Position: %.2f, %.2f\n", x, y)
		fmt.Printf("  Routing Table: %v\n", n.GetRoutingTable())
		fmt.Println()
	}
}
