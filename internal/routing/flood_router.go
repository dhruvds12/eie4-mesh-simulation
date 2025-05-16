package routing

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"mesh-simulation/internal/eventBus"
	"mesh-simulation/internal/mesh"
	"mesh-simulation/internal/packet"
)

// -----------------------------------------------------------------------------
// Managed‑Flood (Meshtastic‑style) Router
// -----------------------------------------------------------------------------
// This router does *not* try to build end‑to‑end routes ahead of time.  Instead
// each DATA or USER_MSG packet is broadcast and opportunistically forwarded by
// every node *once* (subject to TTL/HopLimit).  Duplicates are suppressed via a
// per‑packetID cache, while CSMA/CA and a small random jitter are used to
// minimise collisions.
//
// The implementation purposely fulfils the same IRouter interface as the AODV
// implementation so that `nodeImpl` can swap routers at runtime.
// -----------------------------------------------------------------------------

type FloodRouter struct {
	ownerID uint32

	seenMu     sync.RWMutex
	seenMsgIDs map[uint32]bool // dedup for all packetIDs we have already handled

	// simple global‑user‑table so that the semantics for SendUserMessage remain
	// identical to the AODV implementation. (We reuse the UserEntry definition
	// already present in the routing package.)
	gutMu sync.RWMutex
	gut   map[uint32]UserEntry // key=userID

	eventBus *eventBus.EventBus

	// CSMA parameters (defaults identical to the AODV router but configurable
	// through SetRouterConstants on the node)
	CcaWindow      time.Duration
	CcaSample      time.Duration
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffScheme  string
	BeUnit         time.Duration
	BeMaxExp       int

	pendingMu       sync.RWMutex
	pending         map[uint32]pendingTx
	pendingQuitChan chan struct{}

	broadcastQuitChan chan struct{}
}

// Default config values (mirrors AODV defaults so that the MAC behaviour is
// comparable during experiments).
const (
	defaultCcaWindow      = 5 * time.Millisecond
	defaultCcaSample      = 100 * time.Microsecond
	defaultInitialBackoff = 100 * time.Millisecond
	defaultMaxBackoff     = 2 * time.Second
	defaultBeUnit         = 20 * time.Millisecond
	defaultBeMaxExp       = 5

	defaultTTL = 7 // maximum hop‑count a packet is forwarded
)

// ---------- ACK support ----------
const (
	ackRetryTimeout = 3 * time.Second // time we wait for an (implicit or explicit) ACK
	ackMaxRetrans   = 3               // maximum times we rebroadcast before giving up
)

type pendingTx struct {
	pkt      []byte
	pktID    uint32
	attempts int
	expiry   time.Time
}

// NewFloodRouter constructs a managed‑flood router bound to a specific node ID.
func NewFloodRouter(ownerID uint32, bus *eventBus.EventBus) *FloodRouter {
	return &FloodRouter{
		ownerID:           ownerID,
		seenMsgIDs:        make(map[uint32]bool),
		gut:               make(map[uint32]UserEntry),
		eventBus:          bus,
		CcaWindow:         defaultCcaWindow,
		CcaSample:         defaultCcaSample,
		InitialBackoff:    defaultInitialBackoff,
		MaxBackoff:        defaultMaxBackoff,
		BackoffScheme:     "binary",
		BeUnit:            defaultBeUnit,
		BeMaxExp:          defaultBeMaxExp,
		broadcastQuitChan: make(chan struct{}),
		pending:           make(map[uint32]pendingTx),
		pendingQuitChan:   make(chan struct{}),
	}
}

// -----------------------------------------------------------------------------
// IRouter implementation
// -----------------------------------------------------------------------------

func (r *FloodRouter) SendData(net mesh.INetwork, sender mesh.INode, destID uint32, payload string, flags uint8) {
	payloadBytes := []byte(payload)
	pkt, pktID, err := packet.CreateDataPacket(r.ownerID, r.ownerID, destID, packet.BROADCAST_NH, 0, payloadBytes, flags)
	if err != nil {
		log.Printf("[FloodRouter] Node %d: failed to create DATA packet: %v", r.ownerID, err)
		return
	}
	r.eventBus.Publish(eventBus.Event{Type: eventBus.EventMessageSent, PacketType: packet.PKT_DATA})
	r.BroadcastMessageCSMA(net, sender, pkt, pktID)
	r.trackIfAckRequested(pktID, pkt, flags)
}

func (r *FloodRouter) SendUserMessage(net mesh.INetwork, sender mesh.INode, sendUserID, destUserID uint32, payload string, flags uint8) {
	userEntry, ok := r.getUserEntry(destUserID)
	if !ok {
		// We don't know where the user lives – just flood the message and hope
		// it reaches a node that does.
		userEntry.NodeID = 0 // unknown
	}

	pkt, pktID, err := packet.CreateUSERMessagePacket(r.ownerID, r.ownerID, sendUserID, destUserID, userEntry.NodeID, packet.BROADCAST_NH, 0, []byte(payload), flags)
	if err != nil {
		log.Printf("[FloodRouter] Node %d: failed to create USER_MSG packet: %v", r.ownerID, err)
		return
	}
	r.eventBus.Publish(eventBus.Event{Type: eventBus.EventMessageSent, PacketType: packet.PKT_USER_MSG})
	r.BroadcastMessageCSMA(net, sender, pkt, pktID)
	r.trackIfAckRequested(pktID, pkt, flags)
}

func (r *FloodRouter) HandleMessage(net mesh.INetwork, node mesh.INode, buf []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(buf); err != nil {
		return
	}

	// implicit ACK: we overhear our own PacketID forwarded by somebody else
	r.pendingMu.Lock()
	if _, ok := r.pending[bh.PacketID]; ok && bh.SrcNodeID != r.ownerID {
		delete(r.pending, bh.PacketID)
		r.pendingMu.Unlock()
		r.eventBus.Publish(eventBus.Event{Type: eventBus.EventReceivedDataAck})
	} else {
		r.pendingMu.Unlock()
	}

	// duplicate? (we *always* check for dups except ACKs/BROADCAST_INFO where we
	// might want to forward regardless)
	if r.isSeen(bh.PacketID) {
		return
	}
	r.markSeen(bh.PacketID)

	switch bh.PacketType {
	case packet.PKT_DATA:
		r.handleData(net, node, buf, &bh)
	case packet.PKT_USER_MSG:
		r.handleUserMsg(net, node, buf, &bh)
	case packet.PKT_BROADCAST_INFO:
		r.handleBroadcastInfo(net, node, buf)
	case packet.PKT_ACK:
		// still remove from seen to avoid infinite retransmissions but we don't
		// forward ACKs in flood mode.
		if bh.DestNodeID == r.ownerID {
			// explicit ACK
			_, ah, _ := packet.DeserialiseACKPacket(buf)
			r.pendingMu.Lock()
			delete(r.pending, ah.OriginalPacketID)
			r.pendingMu.Unlock()
			r.eventBus.Publish(eventBus.Event{Type: eventBus.EventReceivedDataAck})
			// ACKs themselves are not forwarded in flood mode
			return
		}

	default:
		// ignore other control frames (RREQ/RREP...) as this router doesn't use
		// them
	}
}

func (r *FloodRouter) trackIfAckRequested(pid uint32, pkt []byte, flags uint8) {
	if flags&packet.REQ_ACK == 0 {
		return
	}
	r.pendingMu.Lock()
	r.pending[pid] = pendingTx{
		pkt:      pkt,
		pktID:    pid,
		attempts: 0,
		expiry:   time.Now().Add(ackRetryTimeout),
	}
	r.pendingMu.Unlock()
	r.eventBus.Publish(eventBus.Event{Type: eventBus.EventRequestedACK})
}

func (r *FloodRouter) sendAck(net mesh.INetwork, node mesh.INode, msgID uint32, originalSender uint32) {
	ackPkt, pid, _ := packet.CreateACKPacket(r.ownerID, originalSender,
		packet.BROADCAST_NH, msgID, 0)
	r.BroadcastMessageCSMA(net, node, ackPkt, pid)
}

func (r *FloodRouter) AddDirectNeighbor(nodeID, neighborID uint32) {}

func (r *FloodRouter) SendBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	r.SendDiffBroadcastInfo(net, node) // we only send the diff style
}

func (r *FloodRouter) SendDiffBroadcastInfo(net mesh.INetwork, node mesh.INode) {
	users := node.GetConnectedUsers()
	added := users // in this simple router we always send full set
	removed := []uint32{}

	pkt, pktID, err := packet.CreateDiffBroadcastInfoPacket(r.ownerID, r.ownerID, added, removed, 0)
	if err != nil {
		log.Printf("[FloodRouter] Node %d: failed to create BROADCAST_INFO packet: %v", r.ownerID, err)
		return
	}
	r.BroadcastMessageCSMA(net, node, pkt, pktID)
	r.eventBus.Publish(eventBus.Event{Type: eventBus.EventControlMessageSent, PacketType: packet.PKT_BROADCAST_INFO})
}

func (r *FloodRouter) PrintRoutingTable() {
	log.Printf("[FloodRouter] Node %d: managed‑flood – no routing table", r.ownerID)
}

func (r *FloodRouter) StartPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	go r.runPendingTxChecker(net, node)
}

func (r *FloodRouter) StopPendingTxChecker() {
	close(r.pendingQuitChan)
}

func (r *FloodRouter) runPendingTxChecker(net mesh.INetwork, node mesh.INode) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-r.pendingQuitChan:
			return
		case <-t.C:
			now := time.Now()
			var retries []pendingTx

			// collect expired entries
			r.pendingMu.Lock()
			for pid, p := range r.pending {
				if now.After(p.expiry) {
					if p.attempts < ackMaxRetrans {
						p.attempts++
						p.expiry = now.Add(ackRetryTimeout)
						r.pending[pid] = p
						retries = append(retries, p)
					} else {
						delete(r.pending, pid) // give up
						r.eventBus.Publish(eventBus.Event{Type: eventBus.EventLostMessage, NodeID: r.ownerID})
					}
				}
			}
			r.pendingMu.Unlock()

			// retransmit outside the lock
			for _, p := range retries {
				r.BroadcastMessageCSMA(net, node, p.pkt, p.pktID)
			}
		}
	}
}

func (r *FloodRouter) StartBroadcastTicker(net mesh.INetwork, node mesh.INode) {
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

func (r *FloodRouter) StopBroadcastTicker()                        {}
func (r *FloodRouter) StopTxWorker()                               {}
func (r *FloodRouter) AddRouteEntry(dest, nextHop uint32, hop int) {}
func (r *FloodRouter) RemoveRouteEntry(dest uint32)                {}
func (r *FloodRouter) SetRoutingParams(th, rreqLim, ureqLim int)   {}

// -----------------------------------------------------------------------------
// Packet handlers
// -----------------------------------------------------------------------------

func (r *FloodRouter) handleData(net mesh.INetwork, node mesh.INode, buf []byte, bh *packet.BaseHeader) {
	_, dh, payload, err := packet.DeserialiseDataPacket(buf)
	if err != nil {
		return
	}

	// deliver locally if final destination matches this node
	if dh.FinalDestID == r.ownerID {
		if bh.Flags == packet.REQ_ACK {
			r.sendAck(net, node, bh.PacketID, dh.OriginNodeID)
		}

		log.Printf("[sim] Node %d: DATA arrived. Payload = %q\n", r.ownerID, payload)

		r.eventBus.Publish(eventBus.Event{Type: eventBus.EventMessageDelivered, NodeID: r.ownerID, Payload: string(payload), OtherNodeID: dh.OriginNodeID, PacketType: packet.PKT_DATA, Timestamp: time.Now()})
		return
	}

	// forward if TTL not exceeded
	if bh.HopCount >= defaultTTL {
		return
	}

	// build a *new* packet with incremented hop count and broadcast again (we
	// keep the original PacketID so that downstream nodes can de‑duplicate).
	fwd, _, _ := packet.CreateDataPacket(dh.OriginNodeID, r.ownerID, dh.FinalDestID, packet.BROADCAST_NH, bh.HopCount+1, payload, bh.Flags, bh.PacketID)

	r.floodAfterJitter(net, node, fwd, bh.PacketID)
}

func (r *FloodRouter) handleUserMsg(net mesh.INetwork, node mesh.INode, buf []byte, bh *packet.BaseHeader) {
	_, umh, payload, err := packet.DeserialiseUSERMessagePacket(buf)
	if err != nil {
		return
	}

	if node.HasConnectedUser(umh.ToUserID) {
		if bh.Flags == packet.REQ_ACK {
			r.sendAck(net, node, bh.PacketID, umh.OriginNodeID)
		}
		log.Printf("[sim] Node %d: USER MESSAGE arrived for user %d from %d. Payload = %q\n", r.ownerID, umh.ToUserID, umh.FromUserID, payload)
		r.eventBus.Publish(eventBus.Event{Type: eventBus.EventMessageDelivered, NodeID: r.ownerID, Payload: string(payload), OtherNodeID: umh.FromUserID, PacketType: packet.PKT_USER_MSG, Timestamp: time.Now()})
		return
	}

	if bh.HopCount >= defaultTTL {
		return
	}

	fwd, _, _ := packet.CreateUSERMessagePacket(umh.OriginNodeID, r.ownerID, umh.FromUserID, umh.ToUserID, umh.ToNodeID, packet.BROADCAST_NH, bh.HopCount+1, payload, bh.Flags, bh.PacketID)
	r.floodAfterJitter(net, node, fwd, bh.PacketID)
}

func (r *FloodRouter) handleBroadcastInfo(net mesh.INetwork, node mesh.INode, buf []byte) {
	var bh packet.BaseHeader
	if err := bh.DeserialiseBaseHeader(buf); err != nil {
		return
	}

	ofs := 16
	var dh packet.DiffBroadcastInfoHeader
	if err := dh.Deserialise(buf[ofs:]); err != nil {
		return
	}
	ofs += 8

	total := int(dh.NumAdded) + int(dh.NumRemoved)
	if len(buf) < ofs+4*total {
		return
	}
	ids := make([]uint32, total)
	for i := 0; i < total; i++ {
		ids[i] = packet.ReadUint32(buf[ofs+i*4:])
	}
	added := ids[:dh.NumAdded]
	// removed := ids[dh.NumAdded:] // not used

	// update GUT
	for _, u := range added {
		r.setUserEntry(u, dh.OriginNodeID)
	}

	// forward if hops < TTL
	if bh.HopCount >= defaultTTL {
		return
	}
	fwd, pid, _ := packet.CreateDiffBroadcastInfoPacket(r.ownerID, dh.OriginNodeID, added, nil, bh.HopCount+1, bh.PacketID)
	r.floodAfterJitter(net, node, fwd, pid)
}

// -----------------------------------------------------------------------------
// Flood‑helper utilities
// -----------------------------------------------------------------------------

func (r *FloodRouter) floodAfterJitter(net mesh.INetwork, sender mesh.INode, pkt []byte, pktID uint32) {
	// random jitter (10–50 ms) to reduce collision probability when many nodes
	// rebroadcast the same frame simultaneously.
	delay := time.Duration(10+rand.Intn(40)) * time.Millisecond
	time.AfterFunc(delay, func() {
		r.BroadcastMessageCSMA(net, sender, pkt, pktID)
	})
}

func (r *FloodRouter) isSeen(id uint32) bool {
	r.seenMu.RLock()
	ok := r.seenMsgIDs[id]
	r.seenMu.RUnlock()
	return ok
}

func (r *FloodRouter) markSeen(id uint32) {
	r.seenMu.Lock()
	r.seenMsgIDs[id] = true
	r.seenMu.Unlock()
}

// ------------------------- GUT helpers ---------------------------------------

func (r *FloodRouter) setUserEntry(userID, nodeID uint32) {
	r.gutMu.Lock()
	r.gut[userID] = UserEntry{NodeID: nodeID, Seq: 0, LastSeen: time.Now()}
	r.gutMu.Unlock()
}

func (r *FloodRouter) getUserEntry(userID uint32) (UserEntry, bool) {
	r.gutMu.RLock()
	ue, ok := r.gut[userID]
	r.gutMu.RUnlock()
	return ue, ok
}

// -----------------------------------------------------------------------------
// CSMA/CA implementation (identical to AODV version so that MAC‑layer behaviour
// remains consistent across routing algorithms)
// -----------------------------------------------------------------------------

func (r *FloodRouter) BroadcastMessageCSMA(net mesh.INetwork, sender mesh.INode, sendPacket []byte, packetID uint32) {
	backoff := r.InitialBackoff
	beExp := 0

	for {
		if r.waitClearChannel(net, sender) {
			log.Printf("[CSMA] Node %d (flood):   Channel idle %v – transmit", r.ownerID, r.CcaWindow)
			net.BroadcastMessage(sendPacket, sender, packetID)
			return
		}

		var wait time.Duration
		switch r.BackoffScheme {
		case "be":
			wait, beExp = r.nextBackoffBE(beExp)
		default:
			wait = time.Duration(rand.Int63n(int64(backoff)))
			backoff = r.nextBackoffBinary(backoff)
		}
		log.Printf("[CSMA] Node %d (flood): busy → wait %v", r.ownerID, wait)
		time.Sleep(wait)
	}
}

func (r *FloodRouter) waitClearChannel(net mesh.INetwork, sender mesh.INode) bool {
	deadline := time.Now().Add(r.CcaWindow)
	for time.Now().Before(deadline) {
		if !net.IsChannelFree(sender) {
			return false
		}
		time.Sleep(r.CcaSample)
	}
	return net.IsChannelFree(sender)
}

func (r *FloodRouter) nextBackoffBinary(cur time.Duration) time.Duration {
	nxt := cur * 2
	if nxt > r.MaxBackoff {
		return r.MaxBackoff
	}
	return nxt
}

func (r *FloodRouter) nextBackoffBE(exp int) (time.Duration, int) {
	if exp > r.BeMaxExp {
		exp = r.BeMaxExp
	}
	slots := 1 << exp
	slot := rand.Intn(slots)
	return time.Duration(slot) * r.BeUnit, exp + 1
}
