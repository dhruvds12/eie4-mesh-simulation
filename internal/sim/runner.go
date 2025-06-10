package sim

import (
	"errors"
	"fmt"
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
	"mesh-simulation/internal/routing"
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
	if r.sc.Routing.RouterType == 1 && r.sc.Traffic.RestrictToKnownRoutes {
		log.Print("RestrictToKnownRoutes MUST be set to false if router is FLOOD")
		return errors.New("RestrictToKnownRoutes MUST be set to false if router is FLOOD")
	}

	network.SetLossProbability(r.sc.Network.LossRate)
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
	// r.wg.Add(1)
	// go func() {
	// 	defer r.wg.Done()
	// 	r.consumeEvents(sub)
	// }()
	go r.consumeEvents(sub)

	// ── start traffic wire up ────────────────────────────────────────────────────
	startTrafficCh := make(chan struct{})

	// ── launch traffic generator ─────────────────────────────────────────
	var once sync.Once
	triggerStart := func() { once.Do(func() { close(startTrafficCh) }) }

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.startTrafficGenerator(startTrafficCh)
	}()

	switch r.sc.Traffic.TrafficStart.Mode {
	case "immediate":
		triggerStart()

	case "after_delay":
		d := r.sc.Traffic.TrafficStart.Delay
		go func() {
			log.Printf("[Runner] waiting %s before traffic start…", d)
			time.Sleep(d)
			triggerStart()
		}()

	case "after_join_count":
		// we'll invoke triggerStart() in the join loop once we've hit the threshold
		// no action here

	default:
		return fmt.Errorf("unknown traffic.start.mode: %q", r.sc.Traffic.TrafficStart.Mode)
	}

	// ── build nodes & users (grid) ──────────────────────────────
	rows := int(math.Ceil(math.Sqrt(float64(r.sc.Nodes.Count))))
	cols := rows
	side := math.Sqrt(r.sc.AreaKm2) // side length of square

	joined := 0
	for rRow := 0; rRow < rows && joined < r.sc.Nodes.Count; rRow++ {
		for cCol := 0; cCol < cols && joined < r.sc.Nodes.Count; cCol++ {
			lat := float64(rRow) * side / float64(rows-1)
			lng := float64(cCol) * side / float64(cols-1)
			// make sure that the node location is in meters not in km (lat and long is not strictly correct view as x an y for this simulation type)
			n := node.NewNode(lat*1000, lng*1000, r.bus, routing.RouterType(r.sc.Routing.RouterType))

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

			joined++
			// if we're in after_join_count mode, fire when threshold reached
			if r.sc.Traffic.TrafficStart.Mode == "after_join_count" &&
				joined >= r.sc.Traffic.TrafficStart.JoinCount {
				log.Printf("[Runner] %d nodes joined → starting traffic\n", joined)
				go func() {
					time.Sleep(r.sc.StartupDelay)
					triggerStart()
				}()
			}

			if d := r.sc.Nodes.JoinDelay; d > 0 {
				time.Sleep(d)
			}
		}
	}

	// ── wait for generator to signal done ────────────────────────────────
	<-r.quit

	// ── end‐of‐run handling ───────────────────────────────────────────────
	if r.sc.EndMode == "drain" {
		deadline := time.Now().Add(r.sc.DrainTimeout)
		const (
			maxZeroChecks = 6000
			checkInterval = 10 * time.Millisecond
		)
		consecZero := 0
		for {
			inFlight := r.net.(*network.NetworkImpl).ActiveTransmissions()
			if inFlight == 0 {
				consecZero++
				if consecZero >= maxZeroChecks {
					break
				}
			} else {
				consecZero = 0
			}
			if time.Now().After(deadline) {
				log.Print("[Runner] drain timeout reached")
				break
			}
			time.Sleep(checkInterval)
		}
	}

	// final teardown
	if leaver, ok := r.net.(interface{ LeaveAll() }); ok {
		leaver.LeaveAll()
	}
	r.wg.Wait()
	return nil
}

// startTrafficGenerator blocks until startCh is closed, then emits traffic
// for the full duration, finally closing r.quit to wake Runner.Run().
func (r *Runner) startTrafficGenerator(startCh <-chan struct{}) {
	<-startCh
	log.Print("[TrafficGen] starting…")

	start := time.Now()
	end := start.Add(r.sc.Duration)

	for {
		now := time.Now()
		if now.After(end) {
			close(r.quit)
			return
		}

		// linear ramp: S → E msgs/node/min
		frac := now.Sub(start).Seconds() / r.sc.Duration.Seconds()
		S, E := r.sc.Traffic.StartMsgPerNodePerMin, r.sc.Traffic.EndMsgPerNodePerMin
		curRPM := S + (E-S)*frac

		λ := curRPM / 60.0 // per‐sec rate per node
		if λ <= 0 {
			λ = 0.1
		}
		interval := time.Duration(float64(time.Second) / (λ * float64(r.sc.Nodes.Count)))

		select {
		case <-time.After(interval):
			r.emitRandomTraffic()
		case <-r.quit:
			return
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
		case eb.EventRequestedACK:
			r.coll.AddRequestedACK()
		case eb.EventReceivedDataAck:
			r.coll.AddReceivedDataAck()
		case eb.EventTxQueueDrop:
			r.coll.AddTxQueueDrop()
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

	var ackFlag uint8
	if r.shouldRequestAck() {
		ackFlag = packet.REQ_ACK
	} else {
		ackFlag = 0
	}

	// pick packet type according to mix
	pt := choosePacket(r.sc.Traffic.PacketMix)
	switch pt {
	case "DATA":
		go from.SendData(r.net, to.GetID(), "hello", ackFlag)
	case "USER_MSG":
		srcUsers := from.GetConnectedUsers()
		if len(srcUsers) == 0 {
			return
		}

		su := srcUsers[rand.Intn(len(srcUsers))] // pick from existing

		var du uint32
		if rand.Float64() < r.sc.Traffic.KnownUserFraction {
			if uid, ok := from.GetRandomKnownUser(); ok {
				du = uid
			} else {
				return

			}
		} else {

			duList := to.GetConnectedUsers()
			if len(duList) == 0 {
				return
			}
			du = duList[rand.Intn(len(duList))] // pick from existing

		}

		go from.SendUserMessage(r.net, su, du, "ping", ackFlag)
	case "BROADCAST":
		// from.SendBroadcastInfo(r.net)
		go from.SendData(r.net, packet.BROADCAST_ADDR, "hello", 0)
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

func (r *Runner) shouldRequestAck() bool {
	// rand.Float64() returns [0.0,1.0)
	return rand.Float64() < r.sc.Traffic.Acks
}

func (r *Runner) Stop() {
	// closing quit can be used to short‐circuit any loops in Run()
	// you’ll need to also check `r.quit` in your Run() select
	close(r.quit)
}
