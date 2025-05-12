package routing

import (
	"log"
	"math/rand"
	"time"

	"mesh-simulation/internal/mesh"
)

// Constants for CSMA

const ccaWindow = 5 * time.Millisecond
const ccaSample = 100 * time.Microsecond

const (
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 5 * time.Second
)

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

	backoff := initialBackoff

	for {
		// channel sensed idle for the whole CCA window?
		if r.waitClearChannel(net, sender) {
			log.Printf("[CSMA] Node %d:   Channel idle %v – transmit",
				r.ownerID, ccaWindow)
			net.BroadcastMessage(sendPacket, sender, packetID)
			return
		}

		// busy: pick a random slot inside current back-off window
		wait := time.Duration(rand.Int63n(int64(backoff)))
		log.Printf("[CSMA] Node %d:   Busy – backoff %v (win=%v)",
			r.ownerID, wait, backoff)
		time.Sleep(wait)

		// binary exponential back-off
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

}

// waitClearChannel returns true iff the channel stayed idle for the
// whole ccaWindow. One failed sample aborts the attempt.
func (r *AODVRouter) waitClearChannel(
	net mesh.INetwork,
	sender mesh.INode) bool {

	deadline := time.Now().Add(ccaWindow)
	for time.Now().Before(deadline) {
		if !net.IsChannelFree(sender) {
			return false // someone started talking – abort
		}
		time.Sleep(ccaSample)
	}

	// need to do a final check before transmitting because might have fallen out of the ccaWindow but haven't check recently
	return net.IsChannelFree(sender) // stayed quiet for full window
}
