package main

import (
	"flag"
	"io"
	"log"
	"os"
	"time"

	eb "mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/metrics"
	"mesh-simulation/internal/network"
	"mesh-simulation/internal/sim"
)

func main() {
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

	// 1. Pick scenario file or quick flags
	cfg := flag.String("scenario", "scenario.yaml", "YAML or JSON scenario description")
	flag.Parse()

	sc, err := sim.LoadScenario(*cfg)
	if err != nil {
		log.Fatalf("scenario: %v", err)
	}

	bus := eb.NewEventBus()
	net := network.NewNetwork(bus)

	metrics.Global = metrics.NewCollector()

	runner := sim.NewRunner(sc, bus, net, metrics.Global)
	if err := runner.Run(); err != nil {
		log.Fatalf("runner: %v", err)
	}

	if err := metrics.Global.Flush(sc.Logging.MetricsFile); err != nil {
		log.Printf("flush‑metrics: %v", err)
	}

	log.Printf("run complete – stats written to %s", sc.Logging.MetricsFile)
}
