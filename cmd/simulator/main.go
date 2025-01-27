package main

import (
	"time"

	"github.com/dhruvds12/eie4-mesh-simulation/internal/simulation"
)

func main() {
	// Simulation parameters
	numNodes := 10               // Number of nodes in the simulation
	duration := 30 * time.Second // Duration of the simulation

	// Start the simulation
	simulation.RunSimulation(numNodes, duration)
}
