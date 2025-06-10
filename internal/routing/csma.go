package routing

import (
	"log"
	"math/rand"
	"time"

	"mesh-simulation/internal/mesh"
)

// Constants for CSMA

// const ccaWindow = 5 * time.Millisecond
// const ccaSample = 100 * time.Microsecond

// const (
// 	initialBackoff = 500 * time.Millisecond
// 	maxBackoff     = 5 * time.Second
// )

// Check that channel is free before sending data to the network
// Will call the broadcast function in the network to send the message to all nodes
func (r *AODVRouter) BroadcastMessageCSMA(net mesh.INetwork, sender mesh.INode, sendPacket []byte, packetID uint32) {
	// backoff := 100 * time.Millisecond
	// // Check if the channel is busy
	// for !net.IsChannelFree(sender) {
	// 	waitTime := time.Duration(1+rand.Intn(int(backoff/time.Millisecond))) * time.Millisecond
	// 	log.Printf("[CSMA] Node %d: Channel busy. Waiting for %v before retrying.\n", r.ownerID, waitTime)
	// 	time.Sleep(waitTime)
	// 	backoff *= 2
	// 	if backoff > 2*time.Second {
	// 		backoff = 2 * time.Second
	// 	}
	// }
	// log.Printf("[CSMA] Node %d: Channel is free. Broadcasting message.\n", r.ownerID)
	// net.BroadcastMessage(sendPacket, sender, packetID)

	jitter := time.Duration(rand.Int63n(int64(100 * time.Millisecond)))
	log.Printf("[CSMA] Node %d: initial jitter %v before CCA\n", r.ownerID, jitter)
	time.Sleep(jitter)

	backoff := r.InitialBackoff
	beExp := 2
	PTransmit := 0.25

	for {
		// channel sensed idle for the whole CCA window?
		if r.waitClearChannel(net, sender) {
			// log.Printf("[CSMA] Node %d:   Channel idle %v – transmit",
			// 	r.ownerID, r.CcaWindow)
			// net.BroadcastMessage(sendPacket, sender, packetID)
			// // the radio is sending therefore we should not allow another packet to come in this is similar to the actual node logic that achieves that relying ont eh DIO1 callback to say tranmission complete
			// radioBusy := 300*time.Millisecond + time.Duration(rand.Intn(10000))*time.Millisecond
			// time.Sleep(radioBusy)
			// return

			if rand.Float64() <= PTransmit {
				log.Printf("[CSMA] Node %d: coin flip OK (p=%.2f) – transmit\n", r.ownerID, PTransmit)
				log.Printf("[CSMA] Node %d:   Channel idle %v – transmit",
					r.ownerID, r.CcaWindow)
				net.BroadcastMessage(sendPacket, sender, packetID)
				// the radio is sending therefore we should not allow another packet to come in this is similar to the actual node logic that achieves that relying ont eh DIO1 callback to say tranmission complete
				radioBusy := 300*time.Millisecond + time.Duration(rand.Intn(1000))*time.Millisecond
				time.Sleep(radioBusy)
				return
			} else {
				log.Printf("[CSMA] Node %d: coin flip NO (p=%.2f) – defer 300ms\n", r.ownerID, PTransmit)
				time.Sleep(300 * time.Millisecond) // hold off one full transmission slot
				continue                           // then retry CCA
			}
		}

		var wait time.Duration
		switch r.BackoffScheme {
		case "be":
			wait, beExp = r.nextBackoffBE(beExp)
		default: // "binary"
			wait = time.Duration(rand.Int63n(int64(backoff)))
			backoff = r.nextBackoffBinary(backoff)
		}
		log.Printf("[CSMA] Node %d: busy → wait %v (scheme=%s)", r.ownerID, wait, r.BackoffScheme)
		time.Sleep(wait)
	}

}

// waitClearChannel returns true iff the channel stayed idle for the
// whole ccaWindow. One failed sample aborts the attempt.
func (r *AODVRouter) waitClearChannel(net mesh.INetwork, sender mesh.INode) bool {

	deadline := time.Now().Add(r.CcaWindow)
	for time.Now().Before(deadline) {
		if !net.IsChannelFree(sender) {
			return false // someone started talking – abort
		}
		time.Sleep(r.CcaSample)
	}

	// need to do a final check before transmitting because might have fallen out of the ccaWindow but haven't check recently
	return net.IsChannelFree(sender) // stayed quiet for full window
}

func (r *AODVRouter) nextBackoffBinary(cur time.Duration) time.Duration {
	nxt := cur * 2
	if nxt > r.MaxBackoff {
		return r.MaxBackoff
	}
	return nxt
}

func (r *AODVRouter) nextBackoffBE(exp int) (time.Duration, int) {
	if exp > r.BeMaxExp {
		exp = r.BeMaxExp
	}
	slots := 1 << exp
	slot := rand.Intn(slots)
	return time.Duration(slot) * r.BeUnit, exp + 1
}
