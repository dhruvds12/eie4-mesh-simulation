package sim

import (
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	eb "mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/metrics"
	"mesh-simulation/internal/network"
	"mesh-simulation/internal/node"
)

type Runner struct {
	sc   *Scenario
	bus  *eb.EventBus
	net  mesh.INetwork
	coll *metrics.Collector

	wg   sync.WaitGroup
	quit chan struct{}
}

func NewRunner(sc *Scenario, bus *eb.EventBus, net mesh.INetwork, coll *metrics.Collector) *Runner {
	return &Runner{sc: sc, bus: bus, net: net, coll: coll, quit: make(chan struct{})}
}

func (r *Runner) Run() error {
	rand.Seed(r.sc.Seed)
	// start network goroutine
	// r.wg.Add(1)
	// go func() { defer r.wg.Done(); r.net.Run() }()
	go r.net.Run()

	// ── build nodes & users ────────────────────────────────────────────────
	// for i := 0; i < r.sc.Nodes.Count; i++ {
	// 	lat := rand.Float64() * r.sc.AreaKm2
	// 	lng := rand.Float64() * r.sc.AreaKm2
	// 	n := node.NewNode(lat, lng, r.bus)
	// 	r.net.Join(n)
	// 	// attach users
	// 	for u := 0; u < r.sc.Users.PerNode; u++ {
	// 		id := uint32(rand.Int31())
	// 		n.AddConnectedUser(id)
	// 	}
	// 	r.wg.Add(1)
	// 	go func(nd mesh.INode) { defer r.wg.Done(); nd.Run(r.net) }(n)
	// }

	// ── metrics wire‑up ────────────────────────────────────────────────────
	sub := r.bus.Subscribe()
	go r.consumeEvents(sub)

	// ── build nodes & users (grid) ──────────────────────────────
	rows := int(math.Ceil(math.Sqrt(float64(r.sc.Nodes.Count))))
	cols := rows
	side := math.Sqrt(r.sc.AreaKm2) // side length of square

	idx := 0
	for rRow := 0; rRow < rows && idx < r.sc.Nodes.Count; rRow++ {
		for cCol := 0; cCol < cols && idx < r.sc.Nodes.Count; cCol++ {
			lat := float64(rRow) * side / float64(rows-1)
			lng := float64(cCol) * side / float64(cols-1)
			n := node.NewNode(lat, lng, r.bus)
			r.net.Join(n)
			for u := 0; u < r.sc.Users.PerNode; u++ {
				n.AddConnectedUser(uint32(rand.Int31()))
			}
			idx++
			if d := r.sc.Nodes.JoinDelay; d > 0 {
				time.Sleep(d)
			}
		}
	}

	if d := r.sc.StartupDelay; d > 0 {
		log.Printf("Startup delay: waiting %s before traffic…", d)
		time.Sleep(d)
	}

	// ── traffic generator ─────────────────────────────────────────────────
	λ := r.sc.Traffic.MsgPerNodePerMin / 60.0 // per‑sec rate per node
	if λ == 0 {
		λ = 0.1
	}
	interval := time.Duration(1e9 / (λ * float64(r.sc.Nodes.Count))) // ns
	tick := time.NewTicker(interval)
	defer tick.Stop()

	done := time.After(r.sc.Duration)

	for {
		select {
		case <-done:
			close(r.quit)
			if leaver, ok := r.net.(interface{ LeaveAll() }); ok {
				leaver.LeaveAll()
			}

			r.wg.Wait()
			return nil
		case <-tick.C:
			r.emitRandomTraffic()
		}
	}
}

func (r *Runner) consumeEvents(ch chan eb.Event) {
	for ev := range ch {
		switch ev.Type {
		case eb.EventMessageSent:
			r.coll.AddSent()
		case eb.EventMessageDelivered:
			r.coll.AddDelivered(ev)
		case eb.EventControlMessageSent:
			r.coll.AddControlSent()
		case eb.EventControlMessageDelivered:
			r.coll.AddControlDelivered(ev)
		}
	}
}

func (r *Runner) emitRandomTraffic() {
	nodeMap := r.net.(*network.NetworkImpl).Nodes
	if len(nodeMap) < 2 {
		return
	}
	keys := make([]mesh.INode, 0, len(nodeMap))
	for _, n := range nodeMap {
		keys = append(keys, n)
	}

	from := keys[rand.Intn(len(keys))]
	to := keys[rand.Intn(len(keys))]
	if from.GetID() == to.GetID() {
		return
	}

	// pick packet type according to mix
	pt := choosePacket(r.sc.Traffic.PacketMix)
	switch pt {
	case "DATA":
		from.SendData(r.net, to.GetID(), "hello")
	case "USER_MSG":
		users := to.GetConnectedUsers()
		if len(users) == 0 {
			return
		}
		du := users[rand.Intn(len(users))]
		su := uint32(rand.Int31())
		from.AddConnectedUser(su)
		from.SendUserMessage(r.net, su, du, "ping")
	case "BROADCAST":
		from.SendBroadcastInfo(r.net)
	}
	// r.coll.AddSent()
}

func choosePacket(m map[string]float64) string {
	p := rand.Float64()
	acc := 0.0
	for k, v := range m {
		acc += v
		if p <= acc {
			return k
		}
	}
	return "DATA"
}
