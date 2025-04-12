package packet

import (
	"encoding/binary"
	"fmt"
	"math/rand"
)

// Packet Types
const (
	PKT_BROADCAST_INFO uint8 = 0x00
	PKT_BROADCAST      uint8 = 0x01
	PKT_RREQ           uint8 = 0x02
	PKT_RREP           uint8 = 0x03
	PKT_RERR           uint8 = 0x04
	PKT_ACK            uint8 = 0x05
	PKT_DATA           uint8 = 0x06
)

const MaxPacketSize = 255

type BaseHeader struct {
	DestNodeID uint32
	SrcNodeID  uint32
	PacketID   uint32
	PacketType uint8
	Flags      uint8
	HopCount   uint8
	Reserved   uint8
}

type RREQHeader struct {
	OriginNodeID   uint32
	RREQDestNodeID uint32
}

type RREPHeader struct {
	OriginNodeID   uint32
	RREPDestNodeID uint32
	Lifetime       uint16
	NumHops        uint8
}

type RERRHeader struct {
	ReporterNodeID     uint32
	BrokenNodeID       uint32
	OriginalDestNodeID uint32
	OriginalPacketID   uint32
	SenderNodeID       uint32
}

type ACKHeader struct {
	OriginalPacketID uint32
}

type DataHeader struct {
	FinalDestID uint32
}

func (bh *BaseHeader) SerialiseBaseHeader() ([]byte, error) {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], bh.DestNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], bh.SrcNodeID)
	binary.LittleEndian.PutUint32(buf[8:12], bh.PacketID)
	buf[12] = bh.PacketType
	buf[13] = bh.Flags
	buf[14] = bh.HopCount
	buf[15] = bh.Reserved
	return buf, nil
}

func (bh *BaseHeader) DeserialiseBaseHeader(buf []byte) error {
	if len(buf) < 16 {
		return fmt.Errorf("buffer too short for BaseHeader")
	}
	bh.DestNodeID = binary.LittleEndian.Uint32(buf[0:4])
	bh.SrcNodeID = binary.LittleEndian.Uint32(buf[4:8])
	bh.PacketID = binary.LittleEndian.Uint32(buf[8:12])
	bh.PacketType = buf[12]
	bh.Flags = buf[13]
	bh.HopCount = buf[14]
	bh.Reserved = buf[15]
	return nil
}

func (rreq *RREQHeader) SerialiseRREQHeader() ([]byte, error) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], rreq.OriginNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rreq.RREQDestNodeID)
	return buf, nil

}

func (rreq *RREQHeader) DeserialiseRREQHeader(buf []byte) error {
	if len(buf) < 8 {
		return fmt.Errorf("buffer too short for RREQHeader")
	}
	rreq.OriginNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rreq.RREQDestNodeID = binary.LittleEndian.Uint32(buf[4:8])
	return nil
}

func (rrep *RREPHeader) SerialiseRREPHeader() ([]byte, error) {
	buf := make([]byte, 11)
	binary.LittleEndian.PutUint32(buf[0:4], rrep.OriginNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rrep.RREPDestNodeID)
	binary.LittleEndian.PutUint16(buf[8:10], rrep.Lifetime)
	buf[10] = rrep.NumHops
	return buf, nil
}

func (rrep *RREPHeader) DeserialiseRREPHeader(buf []byte) error {
	if len(buf) < 11 {
		return fmt.Errorf("buffer too short for RREPHeader")
	}
	rrep.OriginNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rrep.RREPDestNodeID = binary.LittleEndian.Uint32(buf[4:8])
	rrep.Lifetime = binary.LittleEndian.Uint16(buf[8:10])
	rrep.NumHops = buf[10]
	return nil
}

func (rerr *RERRHeader) SerialiseRERRHeader() ([]byte, error) {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], rerr.ReporterNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rerr.BrokenNodeID)
	binary.LittleEndian.PutUint32(buf[8:12], rerr.OriginalDestNodeID)
	binary.LittleEndian.PutUint32(buf[12:16], rerr.OriginalPacketID)
	binary.LittleEndian.PutUint32(buf[16:20], rerr.SenderNodeID)
	return buf, nil
}

func (rerr *RERRHeader) DeserialiseRERRHeader(buf []byte) error {
	if len(buf) < 20 {
		return fmt.Errorf("buffer too short for RERRHeader")
	}
	rerr.ReporterNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rerr.BrokenNodeID = binary.LittleEndian.Uint32(buf[4:8])
	rerr.OriginalDestNodeID = binary.LittleEndian.Uint32(buf[8:12])
	rerr.OriginalPacketID = binary.LittleEndian.Uint32(buf[12:16])
	rerr.SenderNodeID = binary.LittleEndian.Uint32(buf[16:20])
	return nil
}

func (ack *ACKHeader) SerialiseACKHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], ack.OriginalPacketID)
	return buf, nil
}

func (ack *ACKHeader) DeserialiseACKHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for ACKHeader")
	}
	ack.OriginalPacketID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}

func (d *DataHeader) SerialiseDataHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], d.FinalDestID)
	return buf, nil
}

func (d *DataHeader) DeserialiseDataHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for Data header")
	}
	d.FinalDestID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}

func CreateDataPacket(srcID, destID, nextHop uint32, payload []byte) ([]byte, uint32, error) {

	packetID := uint32(rand.Int31())

	bh := BaseHeader{
		DestNodeID: nextHop,
		SrcNodeID:  srcID,
		PacketID:   packetID,
		PacketType: PKT_DATA,
		Flags:      0x0,
		HopCount:   0x0,
		Reserved:   0x0,
	}

	dh := DataHeader{
		FinalDestID: destID,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	dhBytes, err := dh.SerialiseDataHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising DataHeader")
	}

	totalLength := len(bhBytes) + len(dhBytes) + len(payload)
	if totalLength > MaxPacketSize {
		allowedPayloadSize := MaxPacketSize - (len(bhBytes) + len(dhBytes))
		fmt.Printf("Payload too large; truncating from %d to %d bytes\n", len(payload), allowedPayloadSize)
		payload = payload[:allowedPayloadSize]
		totalLength = MaxPacketSize
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], dhBytes)
	offset += len(dhBytes)

	copy(packetBuffer[offset:], payload)

	return packetBuffer, packetID, nil
}
