package utils

import (
	"fmt"
	"runtime"
	"time"
)

// MonitorResources logs resource usage (goroutines and memory) periodically
func MonitorResources(interval time.Duration) {
	go func() {
		var memStats runtime.MemStats
		for {
			runtime.ReadMemStats(&memStats)
			fmt.Printf("[Resource Monitor] Goroutines: %d | HeapAlloc: %.2f KB | HeapObjects: %d\n",
				runtime.NumGoroutine(),
				float64(memStats.HeapAlloc)/1024,
				memStats.HeapObjects,
			)
			time.Sleep(interval)
		}
	}()
}
