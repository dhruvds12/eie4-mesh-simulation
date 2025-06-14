package routing

import (
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"maps"
	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/packet"
)

/*
TODO:
- Implement a timeout for routes
-Implement an ack for data packets (check what meshtastic does)
-Implement RERR (route error) messages when route is broken
*/

type outgoingTx struct {
	pkt    []byte
	pktID  uint32
	sender mesh.INode
	net    mesh.INetwork
}

// RouteEntry stores route info
type RouteEntry struct {
	Destination uint32
	NextHop     uint32
	HopCount    int
}

// Used to store transactions that are pending ACKs
type PendingTx struct {
	MsgID      uint32
	Pkt        []byte
	Dest       uint32 // destination of the data
	NextHop    uint32 // (the broken node)
	Origin     uint32 // original source of the data --> required to send route err back to sender
	Attempts   int
	ExpiryTime time.Time
}

const maxRetrans = 3
const retryTimeout = 30 * time.Second
const lostRoute = 60 * time.Second

type UserEntry struct {
	NodeID   uint32 // home node
	Seq      uint8  // helps to tell us what information to trust ie if received seq is > than current replace if not ignore
	LastSeen time.Time
}

type DataQueueEntry struct {
	packetType uint8
	payload    string
	sendUserID uint32
	destUserID uint32
	flags      uint8
}

type userMessageQueueEntry struct {
	senderID uint32
	payload  string
	flags    uint8
}

// AODVRouter is a per-node router
type AODVRouter struct {
	ownerID           uint32
	RouteMu           sync.RWMutex
	RouteTable        map[uint32]*RouteEntry // key = destination ID
	seenMu            sync.RWMutex
	seenMsgIDs        map[uint32]bool             // deduplicate RREQ/RREP
	dataQueue         map[uint32][]DataQueueEntry // queue of data to send after route is established
	dataMu            sync.RWMutex
	userMessageQueue  map[uint32][]userMessageQueueEntry // queue of data to send after route is established
	queueMu           sync.RWMutex
	pendingTxs        map[uint32]PendingTx
	pendingMu         sync.RWMutex
	broadcastQuitChan chan struct{}
	pendingQuitChan   chan struct{}
	txQuitChan        chan struct{}
	eventBus          *eventBus.EventBus
	gut               map[uint32]UserEntry
	gutMu             sync.RWMutex
	lastUsersShadow   map[uint32]bool
	shadowMu          sync.RWMutex // TODO: is this required
	txQueue           chan outgoingTx

	CcaWindow      time.Duration
	CcaSample      time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffScheme  string
	BeUnit         time.Duration
	BeMaxExp       int

	paramsMu       sync.RWMutex
	ReplyThreshold int // only send RREP/UREP if my route.hops > threshold
	RreqHopLimit   int // max hops we forward a RREQ
	UreqHopLimit   int // max hops we forward a UREQ

	broadcastOnce sync.Once
	pendingOnce   sync.Once
	txOnce        sync.Once

	EnableAdaptiveRERR bool          // if false, revert to “one strike → immediate RERR”
	FailureThreshold   int           // how many timeouts in FailureWindow trigger RERR
	FailureWindow      time.Duration // look back this far when counting failures

	EnableRouteRevalidation bool // if true, check & update route before each retry
	EnableLinkHealthMonitor bool // if true, periodically scan implicit-ACK stats

	// internals
	linkFailureLog map[uint32][]time.Time // keyed by nextHop
	failureMu      sync.Mutex

	linkAckSeen map[uint32]int                  // count of implicit ACKs seen
	linkSentLog map[uint32]map[uint32]time.Time // count of packets sent requiring ACK
	linkMu      sync.Mutex

	brokenMu     sync.Mutex
	brokenNodes  map[uint32]struct{} // set of blacklisted nextHop IDs
	blacklistTTL time.Duration       // how long to avoid a broken node
}

// NewAODVRouter constructs a router for a specific node
func NewAODVRouter(ownerID uint32, bus *eventBus.EventBus) *AODVRouter {
	r := &AODVRouter{
		ownerID:           ownerID,
		RouteTable:        make(map[uint32]*RouteEntry), // TODO: implement a timeout for routes
		seenMsgIDs:        make(map[uint32]bool),
		dataQueue:         make(map[uint32][]DataQueueEntry),
		pendingTxs:        make(map[uint32]PendingTx),
		broadcastQuitChan: make(chan struct{}),
		pendingQuitChan:   make(chan struct{}),
		txQuitChan:        make(chan struct{}),
		eventBus:          bus,
		gut:               make(map[uint32]UserEntry), // global user table -> has the location of a user in the mesh network
		userMessageQueue:  make(map[uint32][]userMessageQueueEntry),
		lastUsersShadow:   make(map[uint32]bool),
		txQueue:           make(chan outgoingTx, 32), // previously 32

		// MAC defaults (will be overwritten by Runner after node creation)
		CcaWindow:      5 * time.Millisecond,
		CcaSample:      100 * time.Microsecond,
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
		BackoffScheme:  "binary",
		BeUnit:         20 * time.Millisecond,
		BeMaxExp:       5,
		brokenNodes:    make(map[uint32]struct{}),
		blacklistTTL:   60 * time.Second,
	}
	r.ReplyThreshold = 2
	r.RreqHopLimit = 10
	r.UreqHopLimit = 10

	r.EnableAdaptiveRERR = false
	r.FailureThreshold = 5
	r.FailureWindow = 60 * time.Second

	r.EnableRouteRevalidation = false
	r.EnableLinkHealthMonitor = false

	r.linkFailureLog = make(map[uint32][]time.Time)
	r.linkAckSeen = make(map[uint32]int)
	r.linkSentLog = make(map[uint32]map[uint32]time.Time)

	if r.EnableLinkHealthMonitor {
		// only start the goroutine should the health monitor be active
		go r.linkHealthChecker()
	}
	go r.txWorker()
	return r
}

func (r *AODVRouter) SetRoutingParams(th, rreqLim, ureqLim int) {
	r.paramsMu.Lock()
	r.ReplyThreshold = th
	r.RreqHopLimit = rreqLim
	r.UreqHopLimit = ureqLim
	r.paramsMu.Unlock()
}

func (r *AODVRouter) ConfigureAdaptiveRERR(on bool, threshold int, window time.Duration) {
	r.EnableAdaptiveRERR = on
	r.FailureThreshold = threshold
	r.FailureWindow = window
}
func (r *AODVRouter) ConfigureRouteRevalidation(on bool) {
	r.EnableRouteRevalidation = on
}
func (r *AODVRouter) ConfigureLinkHealthMonitor(on bool) {
	r.EnableLinkHealthMonitor = on
}

// ------------------------------------------------------------
// runPendingTxChecker periodically checks pendingTxs for expired entries
// If expired, we assume we never overheard the forward => route is broken
func (r *AODVRouter) StartPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	go r.runPendingTxChecker(net, node)
}

func (r *AODVRouter) txWorker() {
	for {
		select {
		case tx := <-r.txQueue:
			r.BroadcastMessageCSMA(tx.net, tx.sender, tx.pkt, tx.pktID)
		case <-r.txQuitChan:
			return
		}
	}
}

// linkHealthChecker periodically evaluates link loss rates
func (r *AODVRouter) linkHealthChecker() {
	ticker := time.NewTicker(120 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !r.EnableLinkHealthMonitor {
			continue
		}
		r.linkMu.Lock()
		for nh, sentMap := range r.linkSentLog {
			sentCount := len(sentMap) + r.linkAckSeen[nh] // total probes
			acked := r.linkAckSeen[nh]
			log.Printf("[LinkHealth] Node %d: on %d, sent: %d received: %d→ KEEP", r.ownerID, nh, sentCount, acked)
			if sentCount > 4 {
				lossRate := 1.0 - float64(acked)/float64(sentCount)
				log.Printf("[LinkHealth] Node %d: loss=%.2f on %d, sent: %d received: %d→ KEEP", r.ownerID, lossRate, nh, sentCount, acked)
				if lossRate > 0.9 {
					log.Printf("[LinkHealth] Node %d: loss=%.2f on %d, sent: %d received: %d→ tearing down", r.ownerID, lossRate, nh, sentCount, acked)
					r.markBroken(nh)
					r.InvalidateRoutes(nh, 0, 0)
				}
			}
			// reset for next window
			delete(r.linkSentLog, nh)
			delete(r.linkAckSeen, nh)
		}
		r.linkMu.Unlock()
	}
}

// recordFailure logs a timeout for a nextHop
func (r *AODVRouter) recordFailure(nh uint32) {
	r.failureMu.Lock()
	defer r.failureMu.Unlock()
	r.linkFailureLog[nh] = append(r.linkFailureLog[nh], time.Now())
}

// recentFailures counts timeouts within FailureWindow
func (r *AODVRouter) recentFailures(nh uint32) int {
	cutoff := time.Now().Add(-r.FailureWindow)
	count := 0
	r.failureMu.Lock()
	defer r.failureMu.Unlock()
	times := r.linkFailureLog[nh]
	var pruned []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			count++
			pruned = append(pruned, t)
		}
	}
	r.linkFailureLog[nh] = pruned
	return count
}

// retry handles retransmission with optional route re-validation
func (r *AODVRouter) retry(tx PendingTx, net mesh.INetwork, node mesh.INode) {
	tx.Attempts++

	// Try to re-validate the route
	re, have := r.getRoute(tx.Dest)
	if have && r.EnableRouteRevalidation {
		if re.NextHop != tx.NextHop {
			oldNH := tx.NextHop
			tx.NextHop = re.NextHop

			// Only patch the BaseHeader if the hop really changed
			var bh packet.BaseHeader
			if err := bh.DeserialiseBaseHeader(tx.Pkt[:20]); err != nil {
				log.Printf("[RETRY] Node %d: corrupt header on retry %d: %v\n", r.ownerID, tx.MsgID, err)
				return
			}
			bh.DestNodeID = tx.NextHop
			newHdr, _ := bh.SerialiseBaseHeader()
			copy(tx.Pkt[:20], newHdr)

			log.Printf("[RETRY] Node %d: route to %d moved from %d → %d, patching header\n",
				r.ownerID, tx.Dest, oldNH, tx.NextHop)
		}
	} else if !have {
		// no route: fire off a fresh RREQ and requeue
		if !r.isBroken(tx.Dest) {
			log.Printf("[RETRY] Node %d: lost route to %d, re-issuing RREQ\n", r.ownerID, tx.Dest)
			r.initiateRREQ(net, node, tx.Dest)
			// tx.Attempts-- // don't count this attempt as we never sent the message
		}
		log.Printf("[RETRY] Node %d: node %d is broken keep packet but do not send\n", r.ownerID, tx.Dest)
		tx.ExpiryTime = time.Now().Add(lostRoute)
		r.pendingMu.Lock()
		r.pendingTxs[tx.MsgID] = tx
		r.pendingMu.Unlock()
		return

	}

	// retransmit
	log.Printf("[RETRY] Node %d: resending msgID=%d (try %d) via %d\n",
		r.ownerID, tx.MsgID, tx.Attempts, tx.NextHop)
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: tx.Pkt, pktID: tx.MsgID}

	// schedule next expiry
	backoff := retryTimeout * time.Duration(tx.Attempts+1)
	jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
	tx.ExpiryTime = time.Now().Add(backoff + jitter)

	r.pendingMu.Lock()
	r.pendingTxs[tx.MsgID] = tx
	r.pendingMu.Unlock()
}

// Checks if a transaction that has been sent has not been ACKed within a certain time
// If not, the route is considered broken
func (r *AODVRouter) runPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	ticker := time.NewTicker(1 * time.Second) // check every 1s
	defer ticker.Stop()

	for {
		select {
		case <-r.pendingQuitChan:
			return
		case <-ticker.C:
			now := time.Now()
			// copy current pending.txs to another temp data structure then iterate over that to prevent holding the lock for too long
			tempPendingTxs := make(map[uint32]PendingTx)
			r.pendingMu.Lock()
			maps.Copy(tempPendingTxs, r.pendingTxs)
			r.pendingMu.Unlock()
			var deleteIds []uint32
			for msgID, tx := range tempPendingTxs {
				// if now.After(tx.ExpiryTime) {

				// 	if tx.Attempts < maxRetrans {
				// 		log.Printf("[RETRY] Node %d resending %d (try %d)",
				// 			r.ownerID, msgID, tx.Attempts+1)
				// 		r.txQueue <- outgoingTx{net: net, sender: node,
				// 			pkt: tx.Pkt, pktID: msgID}

				// 		tx.Attempts++
				// 		backoffBase := retryTimeout * time.Duration(tx.Attempts+1)
				// 		jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
				// 		tx.ExpiryTime = now.Add(backoffBase + jitter)
				// 		r.pendingMu.Lock()
				// 		r.pendingTxs[msgID] = tx
				// 		r.pendingMu.Unlock()
				// 		continue
				// 	} else {
				// 		log.Printf("[Timeout] Node %d: pendingTx expired for msgID=%d => route to %d is considered broken",
				// 			r.ownerID, msgID, tx.Dest)

				// 		// Remove from pendingTxs
				// 		// delete(r.pendingTxs, msgID)
				// 		deleteIds = append(deleteIds, msgID)

				// 		// TODO: RE ENABLE --> removed for testing
				// 		// No nodes are being turned off currently so investigating if large loss of messages is due to
				// 		// incorrect route invalidation.
				// 		// r.InvalidateRoutes(tx.NextHop, tx.Dest, 0)
				// 		// if r.ownerID != tx.Origin {
				// 		// 	route, found := r.getRoute(tx.Origin)
				// 		// 	if found {
				// 		// 		if net != nil && node != nil {
				// 		// 			r.sendRERR(net, node, route.NextHop, tx.Dest, tx.NextHop, msgID, tx.Origin)
				// 		// 		}
				// 		// 	}
				// 		// }

				// 	}

				// }

				if now.Before(tx.ExpiryTime) {
					continue
				}

				// record a failure whenever any packet times out
				if r.EnableAdaptiveRERR {
					r.recordFailure(tx.NextHop)
				}

				if tx.Attempts < maxRetrans {
					// still have retries left
					r.retry(tx, net, node)
					continue
				}

				// no retries left — decide whether to RERR or just drop
				if r.EnableAdaptiveRERR {
					if r.recentFailures(tx.NextHop) < r.FailureThreshold {
						// haven’t yet seen enough link‐wide failures → just drop this pkt
						log.Println("[TIMEOUT] DROPPING PACKET NO RERR YET")
						deleteIds = append(deleteIds, msgID)
						continue
					}
					log.Printf("[TIMEOUT] Node %d Adaptive router RERR for %d", r.ownerID, tx.NextHop)
					// else: threshold reached, fall through to RERR
				}

				// either adaptive is off, or threshold has been reached
				log.Printf("[TIMEOUT] Node %d msgID=%d → link %d broken\n", r.ownerID, msgID, tx.NextHop)
				deleteIds = append(deleteIds, msgID)
				r.InvalidateRoutes(tx.NextHop, tx.Dest, 0)
				if r.ownerID != tx.Origin {
					if rt, ok := r.getRoute(tx.Origin); ok {
						r.sendRERR(net, node, rt.NextHop, tx.Dest, tx.NextHop, msgID, tx.Origin)
					}
				}
			}

			r.pendingMu.Lock()
			for _, msgID := range deleteIds {
				delete(r.pendingTxs, msgID)
			}
			r.pendingMu.Unlock()
		}
	}
}

func (r *AODVRouter) StartBroadcastTicker(net mesh.INetwork, node mesh.INode) {
	go func() {
		t := time.NewTicker(300 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.broadcastQuitChan:
				return
			case <-t.C:
				r.SendDiffBroadcastInfo(net, node)
			}
		}
	}()
}

// Stop the pendingTxChecker
func (r *AODVRouter) StopPendingTxChecker() {
	r.pendingOnce.Do(func() { close((r.pendingQuitChan)) })
}

func (r *AODVRouter) StopBroadcastTicker() {
	r.broadcastOnce.Do(func() { close(r.broadcastQuitChan) })
}

func (r *AODVRouter) StopTxWorker() {
	r.txOnce.Do(func() { close(r.txQuitChan) })
}

// -- IRouter methods --

func (r *AODVRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uint32, payload string, flags uint8) {
	// Check if we already have a route
	if destID != packet.BROADCAST_ADDR {
		if r.isBroken(destID) {
			log.Printf("[blacklist] Node %d: dropping data → broken node %d\n", r.ownerID, destID)
			r.eventBus.Publish(eventBus.Event{Type: eventBus.EventNoRoute, NodeID: r.ownerID})
			return
		}
		// technically need to check next hop is not backlisted
		entry, hasRoute := r.getRoute(destID)
		if !hasRoute {
			// Initiate RREQ
			log.Printf("[sim] Node %d (router) -> no route for %d, initiating RREQ.\n", r.ownerID, destID)
			dqe := DataQueueEntry{
				packetType: packet.PKT_DATA,
				payload:    payload,
				sendUserID: 0,
				destUserID: 0,
				flags:      flags,
			}
			r.dataMu.Lock()
			r.dataQueue[destID] = append(r.dataQueue[destID], dqe)
			r.dataMu.Unlock()
			r.initiateRREQ(net, sender, destID)
			return
		}

		nextHop := entry.NextHop
		payloadBytes := []byte(payload)
		completePacket, packetID, err := packet.CreateDataPacket(r.ownerID, r.ownerID, destID, nextHop, 0, payloadBytes, flags)
		if err != nil {
			log.Printf("[sim] Node %d: failed to create data packet: %v\n", r.ownerID, err)
			return
		}

		log.Printf("[sim] Node %d (router) -> Initiating data to %d via %d\n", r.ownerID, destID, nextHop)
		// r.BroadcastMessageCSMA(net, sender, completePacket, packetID)
		r.eventBus.Publish(eventBus.Event{
			Type:       eventBus.EventMessageSent,
			PacketType: packet.PKT_DATA,
		})
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: sender, pkt: completePacket, pktID: packetID}

		if r.EnableLinkHealthMonitor {

			r.linkMu.Lock()
			m := r.linkSentLog[nextHop]
			if m == nil {
				m = make(map[uint32]time.Time)
				r.linkSentLog[nextHop] = m
			}
			m[packetID] = time.Now()
			r.linkMu.Unlock()

		}

		if flags == packet.REQ_ACK {
			r.eventBus.Publish(eventBus.Event{
				Type: eventBus.EventRequestedACK,
			})
			jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
			expire := time.Now().Add(retryTimeout + jitter) // e.g. 3s
			r.pendingMu.Lock()
			r.pendingTxs[packetID] = PendingTx{
				MsgID:      packetID,
				Pkt:        completePacket,
				Dest:       destID,
				NextHop:    nextHop,
				Origin:     r.ownerID,
				Attempts:   0,
				ExpiryTime: expire,
			}
			r.pendingMu.Unlock()

		}

	} else {
		payloadBytes := []byte(payload)
		completePacket, packetID, err := packet.CreateDataPacket(r.ownerID, r.ownerID, destID, packet.BROADCAST_ADDR, 0, payloadBytes, flags)
		if err != nil {
			log.Printf("[sim] Node %d: failed to create data packet: %v\n", r.ownerID, err)
			return
		}
		r.addSeenPacket(packetID) // prevent re reading a broadcast packet
		r.eventBus.Publish(eventBus.Event{
			Type:       eventBus.EventMessageSent,
			PacketType: packet.PKT_DATA_BROADCAST,
		})
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: sender, pkt: completePacket, pktID: packetID}

		// no acks for broadcast data messages

	}

}

func (r *AODVRouter) SendUserMessage(net mesh.INetwork, sender mesh.INode, sendUserID, destUserID uint32, payload string, flags uint8) {
	// check GUT
	userEntry, ok := r.hasUserEntry(destUserID)
	if r.isBroken(userEntry.NodeID) {
		log.Printf("[blacklist] Node %d: dropping user msg → broken node %d\n", r.ownerID, userEntry.NodeID)
		r.eventBus.Publish(eventBus.Event{Type: eventBus.EventNoRouteUser, NodeID: r.ownerID})
		return
	}

	if !ok {
		// save the message
		log.Printf("[USER MSG] Node %d no route to user %d\n", r.ownerID, destUserID)
		r.saveUsermessage(sendUserID, destUserID, payload, flags)
		r.initiateUREQ(net, sender, destUserID)
		return
	}

	entry, hasRoute := r.getRoute(userEntry.NodeID) // technically need to check that next hop is not blacklisted
	if !hasRoute {
		// Initiate RREQ
		log.Printf("[sim] Node %d (router) sendUserMessage -> no route for %d, initiating RREQ.\n", r.ownerID, userEntry.NodeID)
		dqe := DataQueueEntry{
			packetType: packet.PKT_USER_MSG,
			payload:    payload,
			sendUserID: sendUserID,
			destUserID: destUserID,
			flags:      flags,
		}
		r.dataMu.Lock()
		r.dataQueue[userEntry.NodeID] = append(r.dataQueue[userEntry.NodeID], dqe)
		r.dataMu.Unlock()
		r.initiateRREQ(net, sender, userEntry.NodeID)
		return
	}

	nextHop := entry.NextHop
	payloadBytes := []byte(payload)
	completePacket, packetID, err := packet.CreateUSERMessagePacket(r.ownerID, r.ownerID, sendUserID, destUserID, userEntry.NodeID, nextHop, 0, payloadBytes, flags)
	if err != nil {
		log.Printf("[sim] Node %d: failed to create data packet: %v\n", r.ownerID, err)
		return
	}

	log.Printf("[sim] Node %d (router) -> Initiating user message to %d via %d\n", r.ownerID, userEntry.NodeID, nextHop)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: sender, pkt: completePacket, pktID: packetID}
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventMessageSent,
		PacketType: packet.PKT_USER_MSG,
	})

	if r.EnableLinkHealthMonitor {

		r.linkMu.Lock()
		m := r.linkSentLog[nextHop]
		if m == nil {
			m = make(map[uint32]time.Time)
			r.linkSentLog[nextHop] = m
		}
		m[packetID] = time.Now()
		r.linkMu.Unlock()

	}

	if flags == packet.REQ_ACK {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventRequestedACK,
		})
		jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
		expire := time.Now().Add(retryTimeout + jitter) // e.g. 3s
		r.pendingMu.Lock()
		r.pendingTxs[packetID] = PendingTx{
			MsgID:      packetID,
			Pkt:        completePacket,
			Dest:       userEntry.NodeID,
			NextHop:    nextHop,
			Origin:     r.ownerID,
			Attempts:   0,
			ExpiryTime: expire,
		}
		r.pendingMu.Unlock()

	}

}

// func (r *AODVRouter) SendDataCSMA(net mesh.INetwork, sender mesh.INode, destID uint32, payload string) {
// 	backoff := 100 * time.Millisecond
// 	// Check if the channel is busy
// 	for !net.IsChannelFree(sender) {
// 		waitTime := time.Duration(1+rand.Intn(int(backoff/time.Millisecond))) * time.Millisecond
// 		log.Printf("[CSMA] Node %d: Channel busy. Waiting for %v before retrying.\n", r.ownerID, waitTime)
// 		time.Sleep(waitTime)
// 		backoff *= 2
// 		if backoff > 2*time.Second {
// 			backoff = 2 * time.Second
// 		}
// 	}
// 	log.Printf("[CSMA] Node %d: Channel is free. Sending data.\n", r.ownerID)
// 	r.SendData(net, sender, destID, payload)

// }

// HandleMessage is called when the node receives any message
// TODO: repeated logic should be removed?????
func (r *AODVRouter) HandleMessage(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(receivedPacket); err != nil {
		log.Printf("Node %d: failed to deserialize BaseHeader: %v", r.ownerID, err)
		return
	}
	r.unmarkBroken(bh.OriginNodeID)
	switch bh.PacketType {
	case packet.PKT_BROADCAST_INFO:
		log.Printf("Node %d: received BroadcastInfo\n", r.ownerID)
		r.handleBroadcastInfo(net, node, receivedPacket)
	case packet.PKT_RREQ:
		// All nodes who recieve should handle
		log.Printf("Node %d: received PKT_RREQ\n", r.ownerID)
		r.handleRREQ(net, node, receivedPacket)
	case packet.PKT_RREP:
		// Only the intended recipient should handle
		log.Printf("Node %d: received PKT_RREP\n", r.ownerID)
		if bh.DestNodeID == r.ownerID {
			r.handleRREP(net, node, receivedPacket)
		}
	case packet.PKT_RERR:
		// All nodes who recieve should handle
		log.Printf("Node %d: received PKT_RERR\n", r.ownerID)
		r.handleRERR(net, node, receivedPacket)
	case packet.PKT_ACK: // was data ack can be generalised
		// Only the intended recipient should handle
		if bh.DestNodeID == r.ownerID {
			log.Printf("Node %d: received DATA_ACK\n", r.ownerID)
			//TODO: Explicit acks disabled
			r.HandleDataAck(receivedPacket)
		}
	case packet.PKT_DATA:

		if r.EnableLinkHealthMonitor {
			r.linkMu.Lock()
			if sentMap := r.linkSentLog[bh.PrevHopID]; sentMap != nil {
				if _, seen := sentMap[bh.PacketID]; seen {
					r.linkAckSeen[bh.PrevHopID]++
					delete(sentMap, bh.PacketID)
				}
			}
			r.linkMu.Unlock()
		}

		// Overhearing logic for implicit ACKs
		r.pendingMu.Lock()
		if _, ok := r.pendingTxs[bh.PacketID]; ok {
			// ack
			log.Printf("[Implicit ACK]  Node %d: overheard DATA forward from %d => implicit ack for msgID=%d",
				r.ownerID, bh.PrevHopID, bh.PacketID)
			delete(r.pendingTxs, bh.PacketID)

			r.eventBus.Publish(eventBus.Event{
				Type: eventBus.EventReceivedDataAck,
			})
		}
		r.pendingMu.Unlock()

		// Only the intended recipient should handle
		if bh.DestNodeID == r.ownerID || bh.DestNodeID == packet.BROADCAST_ADDR {
			r.handleDataForward(net, node, receivedPacket)
		}
	case packet.PKT_UREQ:
		r.handleUREQ(net, node, receivedPacket)
	case packet.PKT_UREP:
		r.handleUREP(net, node, receivedPacket)
	case packet.PKT_UERR:
		r.handleUERR(net, node, receivedPacket)
	case packet.PKT_USER_MSG:
		if r.EnableLinkHealthMonitor {
			r.linkMu.Lock()
			if sentMap := r.linkSentLog[bh.PrevHopID]; sentMap != nil {
				if _, seen := sentMap[bh.PacketID]; seen {
					r.linkAckSeen[bh.PrevHopID]++
					delete(sentMap, bh.PacketID)
				}
			}
			r.linkMu.Unlock()
		}

		r.pendingMu.Lock()
		if _, ok := r.pendingTxs[bh.PacketID]; ok {
			// ack
			log.Printf("[Implicit ACK]  Node %d: overheard USER MSG forward from %d => implicit ack for msgID=%d",
				r.ownerID, bh.PrevHopID, bh.PacketID)
			delete(r.pendingTxs, bh.PacketID)

			r.eventBus.Publish(eventBus.Event{
				Type: eventBus.EventReceivedDataAck,
			})
		}
		r.pendingMu.Unlock()

		if bh.DestNodeID == r.ownerID {
			r.handleUserMessage(net, node, receivedPacket)
		}
	default:
		// not a routing message
		log.Printf("[sim] Node %d (router) -> received non-routing message: %d\n", r.ownerID, bh.PacketType)
	}
}

// AddDirectNeighbor is called by the node when it discovers a new neighbor
func (r *AODVRouter) AddDirectNeighbor(nodeID, neighborID uint32) {
	// only do this if nodeID == r.ownerID
	if nodeID != r.ownerID {
		return
	}
	// If we don't have a route, or if this is a shorter route
	existing, exists := r.getRoute(neighborID)
	if !exists || (exists && existing.HopCount > 1) {

		r.AddRouteEntry(neighborID, neighborID, 1)
		log.Printf("[sim] [routing table] Node %d (router) -> direct neighbor: %d\n", r.ownerID, neighborID)
	}
}

func (r *AODVRouter) PrintRoutingTable() {
	fmt.Printf("Node %d (router) -> routing table:\n", r.ownerID)
	r.RouteMu.RLock()
	for dest, route := range r.RouteTable {
		fmt.Printf("   %d -> via %d (hop count %d)\n", dest, route.NextHop, route.HopCount)
	}
	r.RouteMu.RUnlock()

}

func (r *AODVRouter) SendBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	nodeId := node.GetID()
	// Create a unique broadcast ID to deduplicate

	infoPacket, packetID, err := packet.CreateBroadcastInfoPacket(nodeId, nodeId, node.GetConnectedUsers(), 0)
	if err != nil {
		log.Fatalf("Node %d: failed to creare Info packet: %v", nodeId, err)

	}
	// net.BroadcastMessage(m, node)
	// r.BroadcastMessageCSMA(net, node, infoPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: infoPacket, pktID: packetID}
}

func (r *AODVRouter) SendDiffBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	cur := node.GetConnectedUsers()
	now := make(map[uint32]bool, len(cur))
	for _, u := range cur {
		now[u] = true
	}

	r.shadowMu.Lock()

	var added, removed []uint32
	for u := range now {
		if !r.lastUsersShadow[u] {
			added = append(added, u)
		}
	}
	for u := range r.lastUsersShadow {
		if !now[u] {
			removed = append(removed, u)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	r.shadowMu.Unlock()

	pkt, pid, err := packet.CreateDiffBroadcastInfoPacket(
		r.ownerID, r.ownerID, added, removed, 0,
	)
	if err == nil {
		// r.BroadcastMessageCSMA(net, node, pkt, pid)
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: node, pkt: pkt, pktID: pid}
		r.eventBus.Publish(eventBus.Event{
			Type:       eventBus.EventControlMessageSent,
			PacketType: packet.PKT_BROADCAST_INFO,
		})
	}
	r.shadowMu.Lock()
	r.lastUsersShadow = now
	r.shadowMu.Unlock()
}

func (r *AODVRouter) AddRouteEntry(dest, nextHop uint32, hopCount int) {
	re := RouteEntry{
		Destination: dest,
		NextHop:     nextHop,
		HopCount:    hopCount,
	}

	r.insertRouteEntry(dest, re)

	r.eventBus.Publish(eventBus.Event{
		Type:   eventBus.EventAddRouteEntry,
		NodeID: r.ownerID,
		RoutingTableEntry: eventBus.RouteEntry{
			Destination: dest,
			NextHop:     nextHop,
			HopCount:    re.HopCount,
		},
		Timestamp: time.Now(),
	})

}

func (r *AODVRouter) RemoveRouteEntry(dest uint32) {
	route, ok := r.getRoute(dest)

	if !ok {
		return
	}

	r.removeRoute(dest)
	r.eventBus.Publish(eventBus.Event{
		Type:   eventBus.EventRemoveRouteEntry,
		NodeID: r.ownerID,
		RoutingTableEntry: eventBus.RouteEntry{
			Destination: route.Destination,
			NextHop:     route.NextHop,
			HopCount:    route.HopCount,
		},
		Timestamp: time.Now(),
	})

}

// -- Private AODV logic --
// handleBroadcastInfo processes a HELLO broadcast message.
func (r *AODVRouter) handleBroadcastInfo(net mesh.INetwork, node mesh.INode, buf []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(buf); err != nil {
		return
	}
	ofs := 20

	if r.checkSeenPackets(bh.PacketID) {
		// already processed – ignore and DO NOT forward
		return
	}
	r.addSeenPacket(bh.PacketID)

	r.eventBus.Publish(eventBus.Event{
		Type: eventBus.EventControlMessageDelivered,
	})

	if len(buf) < ofs+4 {
		return
	}

	var dh packet.DiffBroadcastInfoHeader
	if err := dh.Deserialise(buf[ofs : ofs+4]); err != nil {
		return
	}
	ofs += 4

	total := int(dh.NumAdded) + int(dh.NumRemoved)
	if len(buf) < ofs+4*total {
		return
	}
	ids := make([]uint32, total)
	for i := 0; i < total; i++ {
		ids[i] = binary.LittleEndian.Uint32(buf[ofs+i*4:])
	}

	added := ids[:dh.NumAdded]
	removed := ids[dh.NumAdded:]

	r.AddDirectNeighbor(node.GetID(), bh.PrevHopID)
	if bh.OriginNodeID != r.ownerID {
		r.maybeAddRoute(bh.OriginNodeID, bh.PrevHopID, int(bh.HopCount)+1)
	}
	for _, u := range added {
		r.addToGUT(u, bh.OriginNodeID)
	}
	for _, u := range removed {
		r.removeUserEntry(u, bh.OriginNodeID)
	}

	if bh.OriginNodeID != r.ownerID && bh.HopCount < packet.MAX_HOPS {
		fwd, pid, _ := packet.CreateDiffBroadcastInfoPacket(
			r.ownerID, bh.OriginNodeID, added, removed,
			bh.HopCount+1, bh.PacketID,
		)
		// r.BroadcastMessageCSMA(net, node, fwd, pid)
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: node, pkt: fwd, pktID: pid}
	}
}

func (r *AODVRouter) initiateRREQ(net mesh.INetwork, sender mesh.INode, destID uint32) {
	rreqPacket, packetID, err := packet.CreateRREQPacket(r.ownerID, destID, r.ownerID, 0)
	if err != nil {
		log.Printf("Error in initRREQ with CreateRREQPacket: %q", err)
		return
	}
	log.Printf("[sim] [RREQ init] Node %d (router) -> initiating RREQ for %d (hop count %d)\n", r.ownerID, destID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	// r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventControlMessageSent,
		PacketType: packet.PKT_RREQ,
	})
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: sender, pkt: rreqPacket, pktID: packetID}
}

// Every Node needs to handle this
func (r *AODVRouter) handleRREQ(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, rh, err := packet.DeserialiseRREQPacket(receivedPacket)
	if err != nil {
		return
	}

	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate RREQ %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)
	// Add reverse route to RREQ source
	r.maybeAddRoute(bh.OriginNodeID, bh.PrevHopID, int(bh.HopCount)+1)

	// if I'm the destination, send RREP
	if r.ownerID == rh.RREQDestNodeID {
		log.Printf("[sim] Node %d: RREQ arrived at destination.\n", r.ownerID)
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		r.sendRREP(net, node, rh.RREQDestNodeID, bh.OriginNodeID, 0) // Should this reset to 0 (yes)
		return
	}

	r.paramsMu.RLock()
	th := r.ReplyThreshold
	r.paramsMu.RUnlock()

	// If we have a route to the destination, we can send RREP
	if route, ok := r.getRoute(rh.RREQDestNodeID); ok && route.HopCount > th {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		r.sendRREP(net, node, rh.RREQDestNodeID, bh.OriginNodeID, route.HopCount)
		return
	}

	r.paramsMu.RLock()
	rhl := r.RreqHopLimit
	r.paramsMu.RUnlock()

	if int(bh.HopCount) >= rhl {
		return // drop – do not propagate further
	}

	fwdRREQ, packetId, err := packet.CreateRREQPacket(r.ownerID, rh.RREQDestNodeID, bh.OriginNodeID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] [RREQ FORWARD] Node %d: forwarding RREQ for %d (hop count %d)\n", r.ownerID, rh.RREQDestNodeID, bh.HopCount)
	// net.BroadcastMessage(fwdMsg, node)
	// r.BroadcastMessageCSMA(net, node, fwdRREQ, packetId)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: fwdRREQ, pktID: packetId}
}

// ONLY use to initate rrep
func (r *AODVRouter) sendRREP(net mesh.INetwork, node mesh.INode, destRREP, sourceRREQ uint32, hopCount int) {
	// find route to 'source' in reverse direction
	reverseRoute, ok := r.getRoute(sourceRREQ)
	if !ok {
		log.Printf("Node %d: can't send RREP, no route to %d.\n", r.ownerID, sourceRREQ)
		return
	}
	rrepPacket, packetID, err := packet.CreateRREPPacket(r.ownerID, destRREP, reverseRoute.NextHop, sourceRREQ, 0, 0, uint8(hopCount))
	if err != nil {
		return
	}
	log.Printf("[sim] [RREP] Node %d: sending RREP to %d via %d current hop count: %d\n", r.ownerID, destRREP, reverseRoute.NextHop, hopCount)
	// net.BroadcastMessage(rrepMsg, node)
	// r.BroadcastMessageCSMA(net, node, rrepPacket, packetID)
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventControlMessageSent,
		PacketType: packet.PKT_RREP,
	})
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: rrepPacket, pktID: packetID}
}

// Only node specified in the RREP message should handle this
func (r *AODVRouter) handleRREP(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise rrep
	bh, rreph, err := packet.DeserialiseRREPPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate RREP %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	// Add forward route to ctrl.Source
	r.maybeAddRoute(rreph.RREPDestNodeID, bh.PrevHopID, int(rreph.NumHops)+1)
	r.maybeAddRoute(bh.PrevHopID, bh.PrevHopID, 1)

	// if I'm the original route requester, done
	if r.ownerID == rreph.RREPDestNodeID {
		log.Printf("Node %d: route to %d established!\n", r.ownerID, bh.OriginNodeID)
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		// create a deep copy of the dataQueue to prevent long mutex lock
		var tempDataQueue []DataQueueEntry
		r.dataMu.Lock()
		tempDataQueue = append(tempDataQueue, r.dataQueue[bh.OriginNodeID]...)
		delete(r.dataQueue, bh.OriginNodeID)
		r.dataMu.Unlock()

		// send any queued data
		for _, dqe := range tempDataQueue {
			if dqe.packetType == packet.PKT_DATA {

				r.SendData(net, node, bh.OriginNodeID, dqe.payload, dqe.flags)
			}

			if dqe.packetType == packet.PKT_USER_MSG {
				r.SendUserMessage(net, node, dqe.sendUserID, dqe.destUserID, dqe.payload, dqe.flags)
			}
		}
		return
	}

	if bh.DestNodeID != r.ownerID {
		log.Printf("[RREP Forward] Node %d Not part of RREP route back\n", r.ownerID)
		return
	}

	// else forward RREP
	reverseRoute, ok := r.getRoute(rreph.RREPDestNodeID)
	if !ok {
		log.Printf("Node %d: got RREP but no route back to %d.\n", r.ownerID, rreph.RREPDestNodeID)
		return
	}

	rrepPacket, packetID, err := packet.CreateRREPPacket(r.ownerID, rreph.RREPDestNodeID, reverseRoute.NextHop, bh.OriginNodeID, 0, bh.HopCount+1, rreph.NumHops+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[RREP FORWARD] Node %d: forwarding RREP to %d via %d\n", r.ownerID, rreph.RREPDestNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdRrep, node)
	// r.BroadcastMessageCSMA(net, node, rrepPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: rrepPacket, pktID: packetID}
}

// ONLY use for initial rerr not suitable for forwarding
func (r *AODVRouter) sendRERR(net mesh.INetwork, node mesh.INode, to uint32, dataDest uint32, brokenNode uint32, packetId uint32, messageSource uint32) {
	r.markBroken(brokenNode)
	rerrPacket, packetID, err := packet.CreateRERRPacket(r.ownerID, to, r.ownerID, brokenNode, dataDest, packetId, messageSource, 0)
	if err != nil {
		return
	}

	log.Printf("[sim] Node %d: sending RERR to %d about broken route for %d.\n", r.ownerID, to, dataDest)
	// net.BroadcastMessage(rerrMsg, node)
	// r.BroadcastMessageCSMA(net, node, rerrPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: rerrPacket, pktID: packetID}
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventControlMessageSent,
		PacketType: packet.PKT_RERR,
	})
}

// Everyone who receives RERR should handle this (only intended recipient should forward to source) (source should not forward)
func (r *AODVRouter) handleRERR(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise bh and rerrHeader
	bh, rerrHeader, err := packet.DeserialiseRERRPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate RERR %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	log.Printf("[sim] Node %d: received RERR => broken node: %d for dest %d\n", r.ownerID, rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID)

	// Invalidate routes
	r.InvalidateRoutes(rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID, bh.PrevHopID)

	if r.ownerID != bh.DestNodeID {
		// Check if node is the intended target
		log.Printf("{RERR} Node %d: received RERR not intended for me.\n", r.ownerID)
		return
	}

	if r.ownerID == bh.OriginNodeID {
		// TODO IS THIS DOUBLE COUNTED <----------------------------------------------------------------------
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventLostMessage,
			NodeID: r.ownerID,
		})

		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		log.Printf("{RERR} Node %d: received RERR for my own message, stopping here.\n", r.ownerID)
		return
	}

	entry, hasRoute := r.getRoute(bh.OriginNodeID)
	if !hasRoute {
		// No route to source, can't forward RERR
		log.Printf("[sim] {RERR FAIL} Node %d: no route to forward RERR destined for %d.\n", r.ownerID, bh.OriginNodeID)
		// TODO: might need to initiate RREQ to source
		return
	}

	// If we have a route to the source of the message, we can forward the RERR
	nexthop := entry.NextHop
	// r.sendRERR(net, node, nexthop, rc.MessageDest, rc.BrokenNode, rc.MessageId, rc.MessageSource)
	rerrPacket, packetID, err := packet.CreateRERRPacket(r.ownerID, nexthop, rerrHeader.ReporterNodeID, rerrHeader.BrokenNodeID, rerrHeader.OriginalDestNodeID, rerrHeader.OriginalPacketID, bh.OriginNodeID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] Node %d: sending RERR to %d about broken route for %d.\n", r.ownerID, nexthop, rerrHeader.OriginalDestNodeID)
	// net.BroadcastMessage(rerrMsg, node)
	// r.BroadcastMessageCSMA(net, node, rerrPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: rerrPacket, pktID: packetID}
	// Message source is in the payload, use existing route to forward RERR
	// This is a simplification, in real AODV we might need to store the "previous hop" for each route

	// if I'm not the source, I forward RERR upstream
	// how do we know who is the source? We might store it or check who gave me data originally
	// For now, let's just forward to the route of rc.Dest's Source if we know it
	// or we can store the msg.GetFrom() as the "previous hop" and forward there
	// If I have a route to the original source of the data, we can forward
	// This part can vary depending on how you track the "source" of data

	// (Simplified) If we do want to forward RERR, we need to know the "previous hop".
	// We might just do nothing if we don't store that.
	// Real AODV would keep track of all active flows or have a route to the source.

	// if we are the original source, we might re-initiate RREQ.
	// But for simplicity, let's stop here.
}

// handleDataForward attempts to forward data if the node isn't the final dest
func (r *AODVRouter) handleDataForward(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	// deserialise Data paacket
	bh, dh, payload, err := packet.DeserialiseDataPacket(receivedPacket)
	if err != nil {
		return
	}

	// doesn't matter if packet is seeen need to send the ack
	if bh.Flags == packet.REQ_ACK && dh.FinalDestID == r.ownerID {
		r.sendDataAck(net, node, bh.PrevHopID, bh.PacketID)
	}

	if r.checkSeenPackets(bh.PacketID) {
		if bh.Flags == packet.REQ_ACK {
			log.Printf("Node %d: sending Data ack as previous failed %d.\n", r.ownerID, bh.PacketID)
			r.sendDataAck(net, node, bh.PrevHopID, bh.PacketID)
			return
		}
		log.Printf("Node %d: ignoring duplicate Data %d.\n", r.ownerID, bh.PacketID)
		return
	}

	r.addSeenPacket(bh.PacketID)
	payloadString := string(payload)
	// If I'm the final destination, do nothing -> the node can "deliver" it

	if dh.FinalDestID == packet.BROADCAST_ADDR {
		log.Printf("[sim] Node %d: DATA BROADCAST arrived. Payload = %q\n", r.ownerID, payload)
		r.eventBus.Publish(eventBus.Event{
			Type:        eventBus.EventMessageDelivered,
			NodeID:      r.ownerID,
			Payload:     payloadString,
			OtherNodeID: bh.OriginNodeID,
			Timestamp:   time.Now(),
			PacketType:  packet.PKT_DATA_BROADCAST,
		})

		if bh.HopCount > packet.DATA_BROADCAST_LIMIT {
			return
		}

		dataPacket, packetID, err := packet.CreateDataPacket(bh.OriginNodeID, r.ownerID, dh.FinalDestID, 0, bh.HopCount+1, payload, 0, bh.PacketID)
		if err != nil {
			return
		}

		log.Printf("[sim] Node %d: forwarding DATA BROADCAST from %d \n", r.ownerID, bh.OriginNodeID)
		// net.BroadcastMessage(fwdMsg, node)
		// r.BroadcastMessageCSMA(net, node, dataPacket, packetID)
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: node, pkt: dataPacket, pktID: packetID}
		return
	}

	if dh.FinalDestID == r.ownerID {
		log.Printf("[sim] Node %d: DATA NODE arrived. Payload = %q\n", r.ownerID, payload)
		// Send an ACK back to the sender
		r.eventBus.Publish(eventBus.Event{
			Type:        eventBus.EventMessageDelivered,
			NodeID:      r.ownerID,
			Payload:     payloadString,
			OtherNodeID: bh.OriginNodeID,
			Timestamp:   time.Now(),
			PacketType:  packet.PKT_DATA,
		})
		// TODO: this is a simplification as this should depend on the packet header -> should not always be sending an ack
		//TODO: Explicit ack disabled
		return
	}

	//TODO: Could this call SendData? (NO -> send data now only for initiation)

	// Otherwise, I should forward it if I have a route

	dest := dh.FinalDestID
	route, ok := r.getRoute(dest)
	if !ok {
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventNoRoute,
			NodeID: r.ownerID,
		})
		// This is in theory an unlikely case because the origin node should have initiated RREQ
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %d: no route to forward DATA destined for %d.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		// no route therefore, we need ot send RERR
		r.sendRERR(net, node, bh.PrevHopID, dest, r.ownerID, bh.PacketID, bh.OriginNodeID)
		return
	}

	dataPacket, packetID, err := packet.CreateDataPacket(bh.OriginNodeID, r.ownerID, dh.FinalDestID, route.NextHop, bh.HopCount+1, payload, bh.Flags, bh.PacketID)
	if err != nil {
		return
	}

	log.Printf("[sim] Node %d: forwarding DATA from %d to %d via %d\n", r.ownerID, bh.PrevHopID, dest, route.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	// r.BroadcastMessageCSMA(net, node, dataPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: dataPacket, pktID: packetID}

	// Implicit ACK: if the next hop is the intended recipient, we can assume the data was received
	if bh.Flags == packet.REQ_ACK {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventRequestedACK,
		})
		// log.Printf("{Implicit ACK} Node %d: overheard forward from %d => implicit ack for msgID=%d", r.ownerID, originID, msg.GetID())
		// TODO: need to wait for an explicit ACK request from sender (simplified)
		jitter := time.Duration(rand.Intn(10000)) * time.Millisecond
		expire := time.Now().Add(retryTimeout + jitter) // e.g. 3s
		r.pendingMu.Lock()
		r.pendingTxs[packetID] = PendingTx{
			MsgID:      packetID,
			Pkt:        dataPacket,
			Dest:       dest,
			NextHop:    route.NextHop,
			Origin:     bh.OriginNodeID,
			Attempts:   0,
			ExpiryTime: expire,
		}
		r.pendingMu.Unlock()
		return
	}

	// // If the next hop is not the destination, we need to track the transaction by overhearing it
	// expire := time.Now().Add(3 * time.Second) // e.g. 3s
	// r.pendingTxs[bh.PacketID] = PendingTx{
	// 	MsgID:               bh.PacketID,
	// 	Dest:                dest,
	// 	NextHop: route.NextHop,
	// 	Origin:              bh.SrcNodeID,
	// 	ExpiryTime:          expire,
	// }

}

func (r *AODVRouter) handleUserMessage(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, umh, payload, err := packet.DeserialiseUSERMessagePacket(receivedPacket)
	if err != nil {
		return
	}

	// if bh.Flags == packet.REQ_ACK {
	// 	// r.sendDataAck(net, node, bh.PrevHopID, bh.PacketID)
	// }

	if bh.Flags == packet.REQ_ACK && umh.ToNodeID == r.ownerID {
		r.sendDataAck(net, node, bh.PrevHopID, bh.PacketID)
	}

	if r.checkSeenPackets(bh.PacketID) {
		if bh.Flags == packet.REQ_ACK {
			log.Printf("Node %d: sending USER Msg ACK as original was lost %d.\n", r.ownerID, bh.PacketID)
			r.sendDataAck(net, node, bh.PrevHopID, bh.PacketID)
		}
		log.Printf("Node %d: ignoring duplicate USER Msg %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	payloadString := string(payload)
	if umh.ToNodeID == r.ownerID {
		// check that the user is at this node
		ok := node.HasConnectedUser(umh.ToUserID)
		if ok {
			log.Printf("[sim] Node %d: USER MESSAGE arrived for user %d from %d. Payload = %q\n", r.ownerID, umh.ToUserID, umh.FromUserID, payload)
			// Send an ACK back to the sender
			r.eventBus.Publish(eventBus.Event{
				Type:        eventBus.EventMessageDelivered, //TODO change to another type to represent a user message
				NodeID:      r.ownerID,
				Payload:     payloadString,
				OtherNodeID: bh.OriginNodeID,
				Timestamp:   time.Now(),
				PacketType:  packet.PKT_USER_MSG,
			})
			// TODO: this is a simplification as this should depend on the packet header -> should not always be sending an ack
			return
		}

		// find route back to sender to send UERR
		route, ok := r.getRoute(bh.OriginNodeID)
		if ok {
			r.eventBus.Publish(eventBus.Event{
				Type:   eventBus.EventUserNotAtNode,
				NodeID: r.ownerID,
			})
			// Th

			r.initiateUERR(net, node, route.NextHop, bh.OriginNodeID, bh.PacketID, umh.ToUserID)
		} else {
			log.Println("[UERR FAILED] Could not send back to user as I have no route back to sender - strange behaviour")
		}
		return
	}

	if bh.DestNodeID != r.ownerID {
		return
	}

	dest := umh.ToNodeID
	route, ok := r.getRoute(dest)
	if !ok {
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventNoRouteUser,
			NodeID: r.ownerID,
		})
		// This is in theory an unlikely case because the origin node should have initiated RREQ
		// No route, we might trigger route discovery or drop
		log.Printf("[sim] Node %d: no route to forward USER MESSAGE destined for %d.\n", r.ownerID, dest)
		// Optionally: r.initiateRREQ(...)
		// no route therefore, we need ot send RERR
		r.sendRERR(net, node, bh.PrevHopID, dest, r.ownerID, bh.PacketID, bh.OriginNodeID)
		return
	}

	userMessagePacket, packetID, err := packet.CreateUSERMessagePacket(bh.OriginNodeID, r.ownerID, umh.FromUserID, umh.ToUserID, umh.ToNodeID, route.NextHop, bh.HopCount+1, payload, bh.Flags, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[USER MSG FORWARD] Node %d forwarding user msg for %d\n", r.ownerID, dest)
	// r.BroadcastMessageCSMA(net, node, userMessagePacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: userMessagePacket, pktID: packetID}

	if bh.Flags == packet.REQ_ACK {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventRequestedACK,
		})
		jitter := time.Duration(rand.Intn(10000)) * time.Millisecond

		expire := time.Now().Add(retryTimeout + jitter) // e.g. 3s
		r.pendingMu.Lock()
		r.pendingTxs[bh.PacketID] = PendingTx{
			MsgID:      bh.PacketID,
			Pkt:        userMessagePacket,
			Dest:       dest,
			NextHop:    route.NextHop,
			Origin:     bh.OriginNodeID,
			Attempts:   0,
			ExpiryTime: expire,
		}
		r.pendingMu.Unlock()

	}

}

// Handle Data ACKs, should remove from pendingTxs
func (r *AODVRouter) HandleDataAck(receivedPacket []byte) {
	// Unpack the payload and remove from pendingTxs
	bh, ack, err := packet.DeserialiseACKPacket(receivedPacket)

	if err != nil {
		return
	}

	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate DATA ACK %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	// log.Printf("{ACK} Node %d: received ACK for msgID=%d\n", r.ownerID, ack.MsgID)
	r.pendingMu.Lock()
	if _, ok := r.pendingTxs[ack.OriginalPacketID]; ok {
		log.Printf("[ACK] Node %d: received ACK for msgID=%d\n", r.ownerID, ack.OriginalPacketID)
		delete(r.pendingTxs, ack.OriginalPacketID)
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventReceivedDataAck,
		})
	}
	r.pendingMu.Unlock()
}

func (r *AODVRouter) initiateUREQ(net mesh.INetwork, sender mesh.INode, targetUser uint32) {
	ureqPacket, packetID, err := packet.CreateUREQPacket(r.ownerID, r.ownerID, targetUser, 0)
	if err != nil {
		log.Printf("Error in initUREQ with CreateUREQPacket: %q", err)
		return
	}
	log.Printf("[sim] [UREQ init] Node %d (router) -> initiating UREQ for %d (hop count %d)\n", r.ownerID, targetUser, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	// r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: sender, pkt: ureqPacket, pktID: packetID}
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventControlMessageSent,
		PacketType: packet.PKT_UREQ,
	})
}

func (r *AODVRouter) initiateUREP(net mesh.INetwork, sender mesh.INode, destID, ureqOrigin uint32, targetUser uint32) {
	urepPacket, packetID, err := packet.CreateUREPPacket(r.ownerID, destID, ureqOrigin, r.ownerID, targetUser, 0, 0)
	if err != nil {
		log.Printf("Error in initUREP with CreateUREPPacket: %q", err)
		return
	}
	log.Printf("[sim] [UREP init] Node %d (router) -> initiating UREP for %d (hop count %d)\n", r.ownerID, destID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	// r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: sender, pkt: urepPacket, pktID: packetID}

}

func (r *AODVRouter) initiateUERR(net mesh.INetwork, sender mesh.INode, destNodeID, originNodeID, originalPacketID uint32, UERRUserID uint32) {
	uerrPacket, packetID, err := packet.CreateUERRPacket(r.ownerID, destNodeID, UERRUserID, r.ownerID, originNodeID, originalPacketID)
	if err != nil {
		log.Printf("Error in init UERR with CreateUERRPacket: %q", err)
		return
	}
	log.Printf("[sim] [UERR init] Node %d (router) -> initiating UERR for %d (hop count %d)\n", r.ownerID, destNodeID, 0)
	// net.BroadcastMessage(rreqMsg, sender)
	// r.BroadcastMessageCSMA(net, sender, rreqPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: sender, pkt: uerrPacket, pktID: packetID}
	r.eventBus.Publish(eventBus.Event{
		Type:       eventBus.EventControlMessageSent,
		PacketType: packet.PKT_UERR,
	})
}

func (r *AODVRouter) handleUREQ(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUREQPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate UREQ %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	// add routes

	r.maybeAddRoute(bh.PrevHopID, bh.PrevHopID, 1)
	r.maybeAddRoute(bh.OriginNodeID, bh.PrevHopID, int(bh.HopCount)+1)

	if node.HasConnectedUser(uh.UREQUserID) {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		// user connected to me return UREQ
		log.Printf("[sim] [UREQ] Node %d: has user connected %d (hop count %d), sending to origin %d\n", r.ownerID, uh.UREQUserID, bh.HopCount+1, bh.OriginNodeID)
		reply, pid, _ := packet.CreateUREPPacket(r.ownerID, bh.PrevHopID, bh.OriginNodeID, r.ownerID, uh.UREQUserID, 0, 0)
		// r.BroadcastMessageCSMA(net, node, reply, pid)
		// TODO need to drop if the queue is full
		r.txQueue <- outgoingTx{net: net, sender: node, pkt: reply, pktID: pid}
		r.eventBus.Publish(eventBus.Event{
			Type:       eventBus.EventControlMessageSent,
			PacketType: packet.PKT_UREP,
		})
		return
	}

	// If I have a path to user reply
	if n, ok := r.hasUserEntry(uh.UREQUserID); ok {
		r.paramsMu.RLock()
		th := r.ReplyThreshold
		r.paramsMu.RUnlock()

		route, have := r.getRoute(n.NodeID)
		if have && route.HopCount > th {
			r.eventBus.Publish(eventBus.Event{
				Type: eventBus.EventControlMessageDelivered,
			})
			// reply with UREP along reverse path
			log.Printf("[sim] [UREQ] Node %d: has ROUTE userr %d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount+1)
			reply, pid, _ := packet.CreateUREPPacket(r.ownerID, bh.PrevHopID, bh.OriginNodeID, n.NodeID, uh.UREQUserID, 0, uint8(route.HopCount))
			// r.BroadcastMessageCSMA(net, node, reply, pid)
			// TODO need to drop if the queue is full
			r.txQueue <- outgoingTx{net: net, sender: node, pkt: reply, pktID: pid}
			r.eventBus.Publish(eventBus.Event{
				Type:       eventBus.EventControlMessageSent,
				PacketType: packet.PKT_UREP,
			})
			return
		}

		log.Printf("[sim] [UREQ] Node %d: has ROUTE to user %d BUT DID NOT SHARE AS IN REPLYTHRESHOLD (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount+1)

		// send user home node info anyway --> not sure about this clearly no longer connected to the home node of the user and could be
		// outdated information --> therefore should not send
		// if !have {
		// 	r.eventBus.Publish(eventBus.Event{
		// 		Type: eventBus.EventControlMessageDelivered,
		// 	})
		// 	// reply with UREP along reverse path
		// 	log.Printf("[sim] [UREQ] Node %d: has ROUTE userr %d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount+1)
		// 	reply, pid, _ := packet.CreateUREPPacket(r.ownerID, bh.SrcNodeID, uh.OriginNodeID, n.NodeID, uh.UREQUserID, 0, 0)
		// 	// r.BroadcastMessageCSMA(net, node, reply, pid)
		// 	r.txQueue <- outgoingTx{net: net, sender: node, pkt: reply, pktID: pid}
		// 	r.eventBus.Publish(eventBus.Event{
		// 		Type:       eventBus.EventControlMessageSent,
		// 		PacketType: packet.PKT_UREP,
		// 	})
		// 	return
		// }
	}

	r.paramsMu.RLock()
	uhl := r.UreqHopLimit
	r.paramsMu.RUnlock()

	if int(bh.HopCount) >= uhl {
		return
	}

	// Otherwise forward the rreq
	fwdUREQ, packetID, err := packet.CreateUREQPacket(r.ownerID, bh.OriginNodeID, uh.UREQUserID, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[sim] [UREQ FORWARD] Node %d: forwarding UREQ for %d (hop count %d)\n", r.ownerID, uh.UREQUserID, bh.HopCount+1)
	// net.BroadcastMessage(fwdMsg, node)
	// r.BroadcastMessageCSMA(net, node, fwdUREQ, packetId)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: fwdUREQ, pktID: packetID}

}

func (r *AODVRouter) handleUREP(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUREPPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate UREP %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	if r.ownerID == bh.OriginNodeID {
		r.eventBus.Publish(eventBus.Event{
			Type: eventBus.EventControlMessageDelivered,
		})
		// store in the GUT
		r.addToGUT(uh.UREPUserID, uh.UREPDestNodeID)

		// add to router
		r.maybeAddRoute(uh.UREPDestNodeID, bh.PrevHopID, int(bh.HopCount)+1)

		// do some thing like send the messages that were queued

		msgs := r.getUserMessages(uh.UREPUserID)
		for _, umqe := range msgs {
			log.Printf("[USER MESSAGE] Complete UREQ sending message to user %d from user %d", uh.UREPUserID, umqe.senderID)
			r.SendUserMessage(net, node, umqe.senderID, uh.UREPUserID, umqe.payload, umqe.flags)
		}
		return

	}

	if bh.DestNodeID != r.ownerID {
		return
	}
	// find next hop from routing table - should have way back
	reverseRoute, ok := r.getRoute(uh.UREPDestNodeID)
	if !ok {
		log.Printf("Node %d: got UREP but no route back to %d.\n", r.ownerID, uh.UREPDestNodeID)
		return
	}

	// Otherwise forward the rreq
	fwdUREP, packetID, err := packet.CreateUREPPacket(r.ownerID, reverseRoute.NextHop, bh.OriginNodeID, uh.UREPDestNodeID, uh.UREPUserID, 0, bh.HopCount+1, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[UREP FORWARD] Node %d: forwarding UREP to %d via %d\n", r.ownerID, uh.UREPDestNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	// r.BroadcastMessageCSMA(net, node, fwdUREP, packetId)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: fwdUREP, pktID: packetID}

}

func (r *AODVRouter) handleUERR(net mesh.INetwork, node mesh.INode, receivedPacket []byte) {
	bh, uh, err := packet.DeserialiseUERRPacket(receivedPacket)
	if err != nil {
		return
	}
	if r.checkSeenPackets(bh.PacketID) {
		log.Printf("Node %d: ignoring duplicate UERR %d.\n", r.ownerID, bh.PacketID)
		return
	}
	r.addSeenPacket(bh.PacketID)

	if r.ownerID == bh.OriginNodeID {
		// r.eventBus.Publish(eventBus.Event{
		// 	Type: eventBus.EventMessageDelivered,
		// })

		// TODO IS THIS DOUBLE COUNTED <--------------------------------------------------------------
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventLostMessage,
			NodeID: r.ownerID,
		})
		r.removeUserEntry(uh.UserID, uh.NodeID)
		log.Printf("[UERR RECEIVED] Node %d received a uerr ", r.ownerID)
		// should not remove route as a UERR is only tiggered
		r.pendingMu.Lock()
		delete(r.pendingTxs, uh.OriginalPacketID)
		r.pendingMu.Unlock()
		return
	}

	// else forward the message to destination
	reverseRoute, ok := r.getRoute(bh.OriginNodeID)
	if !ok {
		log.Printf("Node %d: got UERR but no route back to %d.\n", r.ownerID, bh.OriginNodeID)
		return
	}

	// Otherwise forward the rreq
	fwdUERR, packetID, err := packet.CreateUERRPacket(r.ownerID, reverseRoute.NextHop, uh.UserID, uh.NodeID, uh.OriginalPacketID, bh.PacketID)
	if err != nil {
		return
	}
	log.Printf("[UERR FORWARD] Node %d: forwarding UERR to %d via %d\n", r.ownerID, bh.OriginNodeID, reverseRoute.NextHop)
	// net.BroadcastMessage(fwdMsg, node)
	// r.BroadcastMessageCSMA(net, node, fwdUREP, packetId)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: fwdUERR, pktID: packetID}

}

func (r *AODVRouter) InvalidateRoutes(brokenNode uint32, dest uint32, sender uint32) {
	r.RouteMu.Lock()
	defer r.RouteMu.Unlock()
	// remove route to destination node if it goes through the sender
	if sender != 0 {
		if re, ok := r.RouteTable[dest]; ok && re.NextHop == sender {

			log.Printf("Node %d: removing route to %d because it goes through broken node %d.\n", r.ownerID, dest, sender)
			delete(r.RouteTable, dest)

			r.eventBus.Publish(eventBus.Event{
				Type:   eventBus.EventRemoveRouteEntry,
				NodeID: r.ownerID,
				RoutingTableEntry: eventBus.RouteEntry{
					Destination: re.Destination,
					NextHop:     re.NextHop,
					HopCount:    re.HopCount,
				},
				Timestamp: time.Now(),
			})

		}
	}

	// Remove any direct route to the broken node.
	if route, ok := r.RouteTable[brokenNode]; ok {
		log.Printf("Node %d: removing direct route to broken node %d.\n", r.ownerID, brokenNode)
		delete(r.RouteTable, brokenNode)
		r.eventBus.Publish(eventBus.Event{
			Type:   eventBus.EventRemoveRouteEntry,
			NodeID: r.ownerID,
			RoutingTableEntry: eventBus.RouteEntry{
				Destination: route.Destination,
				NextHop:     route.NextHop,
				HopCount:    route.HopCount,
			},
			Timestamp: time.Now(),
		})
	}

	// Iterate through the routing table.
	for dest, route := range r.RouteTable {
		if route.NextHop == brokenNode {
			log.Printf("Node %d: invalidating route to %d because NextHop %d is broken.\n", r.ownerID, dest, brokenNode)
			delete(r.RouteTable, dest)
			r.eventBus.Publish(eventBus.Event{
				Type:   eventBus.EventRemoveRouteEntry,
				NodeID: r.ownerID,
				RoutingTableEntry: eventBus.RouteEntry{
					Destination: route.Destination,
					NextHop:     route.NextHop,
					HopCount:    route.HopCount,
				},
				Timestamp: time.Now(),
			})
		}
	}

}

// send ack for data packets
func (r *AODVRouter) sendDataAck(net mesh.INetwork, node mesh.INode, to uint32, prevMsgId uint32) {

	// if true {
	// 	log.Println("[sim] disabled explicit acks")
	// 	return
	// }

	// route, ok := r.getRoute(to)
	// if !ok {
	// 	log.Printf("[sim] Node %d: no route to send data ack destined for %d.\n", r.ownerID, to)
	// 	return
	// }

	ackPacket, packetID, err := packet.CreateACKPacket(r.ownerID, to, to, prevMsgId, 0)

	if err != nil {
		return
	}

	log.Printf("[sim] Node %d: sending DATA_ACK to %d\n", r.ownerID, to)
	// r.BroadcastMessageCSMA(net, node, ackPacket, packetID)
	// TODO need to drop if the queue is full
	r.txQueue <- outgoingTx{net: net, sender: node, pkt: ackPacket, pktID: packetID}
}

// maybeAddRoute updates route if shorter
func (r *AODVRouter) maybeAddRoute(dest, nextHop uint32, hopCount int) {
	exist, ok := r.getRoute(dest)
	if !ok || hopCount < exist.HopCount {
		// log an update to the routing table
		log.Printf("[sim] [routing table] Node %d (router) -> updated route to %d via %d (hop count %d)\n", r.ownerID, dest, nextHop, hopCount)

		r.AddRouteEntry(dest, nextHop, hopCount)
	}
}

func (r *AODVRouter) addToGUT(userId, nodeId uint32) {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	r.gut[userId] = UserEntry{
		NodeID:   nodeId,
		Seq:      0, // set to 0 for now
		LastSeen: time.Now(),
	}
}

func (r *AODVRouter) hasUserEntry(userId uint32) (UserEntry, bool) {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	if userEntry, ok := r.gut[userId]; ok {
		return userEntry, ok
	}

	return UserEntry{}, false

}

func (r *AODVRouter) removeUserEntry(userId uint32, nodeId uint32) bool {
	r.gutMu.Lock()
	defer r.gutMu.Unlock()

	// delete from GUT
	if userEntry, ok := r.gut[userId]; ok {
		if userEntry.NodeID == nodeId {
			delete(r.gut, userId)
			return true
		}
	}
	return false
}

func (r *AODVRouter) saveUsermessage(senderUserID, destUserId uint32, payload string, flags uint8) {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()

	umqe := userMessageQueueEntry{
		senderID: senderUserID,
		payload:  payload,
		flags:    flags,
	}

	r.userMessageQueue[destUserId] = append(r.userMessageQueue[destUserId], umqe)
}

// getUserMessages returns all buffered messages for userId and clears them out.
func (r *AODVRouter) getUserMessages(userId uint32) []userMessageQueueEntry {
	r.queueMu.Lock()
	defer r.queueMu.Unlock()

	msgs := r.userMessageQueue[userId]
	// clear the queue
	delete(r.userMessageQueue, userId)
	return msgs
}

func (r *AODVRouter) getRoute(NodeID uint32) (RouteEntry, bool) {
	r.RouteMu.RLock()
	entry, ok := r.RouteTable[NodeID]
	r.RouteMu.RUnlock()

	if ok {
		return *entry, ok
	}

	return RouteEntry{}, ok
}

func (r *AODVRouter) insertRouteEntry(NodeID uint32, re RouteEntry) {
	r.RouteMu.Lock()
	r.RouteTable[NodeID] = &re
	r.RouteMu.Unlock()
}

func (r *AODVRouter) removeRoute(NodeID uint32) {
	r.RouteMu.Lock()
	delete(r.RouteTable, NodeID)
	r.RouteMu.Unlock()

}

func (r *AODVRouter) checkSeenPackets(packetID uint32) bool {
	r.seenMu.RLock()
	ans := r.seenMsgIDs[packetID]
	r.seenMu.RUnlock()
	return ans
}

func (r *AODVRouter) addSeenPacket(packetID uint32) {
	r.seenMu.Lock()
	r.seenMsgIDs[packetID] = true
	r.seenMu.Unlock()
}

func (r *AODVRouter) GUTSnapshot() map[uint32]UserEntry {
	r.gutMu.RLock()
	defer r.gutMu.RUnlock()
	// shallow copy is fine for just keys
	snap := make(map[uint32]UserEntry, len(r.gut))
	for k, v := range r.gut {
		snap[k] = v
	}
	return snap
}

func (r *AODVRouter) markBroken(nodeID uint32) {
	r.brokenMu.Lock()
	if _, ok := r.brokenNodes[nodeID]; !ok {
		r.brokenNodes[nodeID] = struct{}{}
		// schedule auto-unblacklist
		// go func() {
		//     time.Sleep(r.blacklistTTL)
		//     r.brokenMu.Lock()
		//     delete(r.brokenNodes, nodeID)
		//     r.brokenMu.Unlock()
		// }()
	}
	r.brokenMu.Unlock()
}

func (r *AODVRouter) unmarkBroken(nodeID uint32) {
	r.brokenMu.Lock()
	delete(r.brokenNodes, nodeID)
	r.brokenMu.Unlock()
}

func (r *AODVRouter) isBroken(nodeID uint32) bool {
	r.brokenMu.Lock()
	_, bad := r.brokenNodes[nodeID]
	r.brokenMu.Unlock()
	return bad
}
