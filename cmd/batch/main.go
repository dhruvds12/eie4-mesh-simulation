package main

import (
    "flag"
    "log"

    eb "mesh-simulation/internal/eventBus"
    "mesh-simulation/internal/metrics"
    "mesh-simulation/internal/network"
    "mesh-simulation/internal/sim"
)

func main() {
    // 1. Pick scenario file or quick flags
    cfg := flag.String("scenario", "scenario.yaml", "YAML or JSON scenario description")
    flag.Parse()

    sc, err := sim.LoadScenario(*cfg)
    if err != nil { log.Fatalf("scenario: %v", err) }

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