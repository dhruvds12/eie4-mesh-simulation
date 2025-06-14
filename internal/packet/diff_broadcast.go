package packet

import (
	"encoding/binary"
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────────────
//
//	Diff-BroadcastInfo header  -- matches ESP32 firmware byte-for-byte
//
// ──────────────────────────────────────────────────────────────────────────────
type DiffBroadcastInfoHeader struct {
	NumAdded     uint16
	NumRemoved   uint16
}

func (h *DiffBroadcastInfoHeader) Serialise() []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint16(buf[0:2], h.NumAdded)
	binary.LittleEndian.PutUint16(buf[2:4], h.NumRemoved)
	return buf
}

func (h *DiffBroadcastInfoHeader) Deserialise(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for DiffBroadcastInfoHeader")
	}
	h.NumAdded = binary.LittleEndian.Uint16(buf[0:2])
	h.NumRemoved = binary.LittleEndian.Uint16(buf[2:4])
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
//
//	Packet builder
//
// ──────────────────────────────────────────────────────────────────────────────
func CreateDiffBroadcastInfoPacket(
	srcID uint32,
	originNodeID uint32,
	added []uint32,
	removed []uint32,
	hopCount uint8,
	packetID ...uint32,
) ([]byte, uint32, error) {

	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID: BROADCAST_ADDR,
		OriginNodeID: originNodeID,
		PrevHopID:  srcID,
		PacketID:   pid,
		PacketType: PKT_BROADCAST_INFO,
		HopCount:   hopCount,
		Flags: 0,
		Reserved: 0,
	}

	dh := DiffBroadcastInfoHeader{
		NumAdded:     uint16(len(added)),
		NumRemoved:   uint16(len(removed)),
	}

	bhBytes, _ := bh.SerialiseBaseHeader()
	dhBytes := dh.Serialise()

	total := len(bhBytes) + len(dhBytes) + 4*(len(added)+len(removed))
	if total > MaxPacketSize {
		return nil, 0, fmt.Errorf("DiffBroadcastInfo packet too big (%d B)", total)
	}

	pkt := make([]byte, total)
	copy(pkt, bhBytes)
	ofs := len(bhBytes)
	copy(pkt[ofs:], dhBytes)
	ofs += len(dhBytes)

	for _, id := range added {
		binary.LittleEndian.PutUint32(pkt[ofs:], id)
		ofs += 4
	}
	for _, id := range removed {
		binary.LittleEndian.PutUint32(pkt[ofs:], id)
		ofs += 4
	}
	return pkt, pid, nil
}
