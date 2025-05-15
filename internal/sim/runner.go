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
	"mesh-simulation/internal/packet"
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

			if ok := n.SetRouterConstants(
				r.sc.CSMA.CCAWindow,
				r.sc.CSMA.CCASample,
				r.sc.CSMA.InitialBackoff,
				r.sc.CSMA.MaxBackoff,
				r.sc.CSMA.BackoffScheme,
				r.sc.CSMA.BEUnit,
				r.sc.CSMA.BEMaxExp,
			); !ok {
				log.Println("Failed to add the CSMA configuration!")
			}

			if ok := n.SetRoutingParams(
				r.sc.Routing.ReplyThresholdHops,
				r.sc.Routing.RREQHopLimit,
				r.sc.Routing.UREQHopLimit,
			); !ok {
				log.Println("Failed to add the Routing Params configuration!")
			}

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
			tick.Stop() // stop creating new traffic

			if r.sc.EndMode == "drain" {
				deadline := time.Now().Add(r.sc.DrainTimeout)
				for time.Now().Before(deadline) {
					// if r.net.(*network.NetworkImpl).ActiveTransmissions() == 0 {
					// 	time.Sleep(30 * time.Millisecond)
					// 	break
					// }
					time.Sleep(10 * time.Millisecond)
				}
			}
			log.Printf("Remaining active transmissions on close: %d", r.net.(*network.NetworkImpl).ActiveTransmissions())
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
			r.coll.AddSent(ev)
		case eb.EventMessageDelivered:
			r.coll.AddDelivered(ev)
		case eb.EventControlMessageSent:
			r.coll.AddControlSent(ev)
		case eb.EventControlMessageDelivered:
			r.coll.AddControlDelivered(ev)
		case eb.EventLostMessage:
			r.coll.AddLostMessage()
		case eb.EventNoRoute:
			r.coll.AddNoRoute()
		case eb.EventNoRouteUser:
			r.coll.AddNoRouteUser()
		case eb.EventUserNotAtNode:
			r.coll.AddUserNotAtNode()
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

	var to mesh.INode
	if r.sc.Traffic.RestrictToKnownRoutes {
		if id, ok := from.GetRandomKnownNode(); ok {
			if nd, err := r.net.(*network.NetworkImpl).GetNode(id); err == nil {
				to = nd
			}
		}
		if to == nil {
			return
		} // no known destinations yet
	} else {
		to = keys[rand.Intn(len(keys))]
		if from.GetID() == to.GetID() {
			return
		}
	}

	// pick packet type according to mix
	pt := choosePacket(r.sc.Traffic.PacketMix)
	switch pt {
	case "DATA":
		from.SendData(r.net, to.GetID(), "hello", packet.REQ_ACK)
	case "USER_MSG":
		users := to.GetConnectedUsers()
		if len(users) == 0 {
			return
		}
		du := users[rand.Intn(len(users))]
		su := uint32(rand.Int31())
		from.AddConnectedUser(su)
		from.SendUserMessage(r.net, su, du, "ping", packet.REQ_ACK)
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
