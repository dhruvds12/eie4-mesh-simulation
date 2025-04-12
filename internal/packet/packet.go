package packet

import (
	"encoding/binary"
	"fmt"
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

type BaseHeader struct {
	destNodeID uint32
	srcNodeID  uint32
	packetID   uint32
	packetType uint8
	flags      uint8
	hopCount   uint8
	reserved   uint8
}

type RREQHeader struct {
	originNodeID   uint32
	RREQDestNodeID uint32
}

type RREPHeader struct {
	originNodeID   uint32
	RREPDestNodeID uint32
	lifetime       uint16
	numHops        uint8
}

type RERRHeader struct {
	reporterNodeID     uint32
	brokenNodeID       uint32
	originalDestNodeID uint32
	originalPacketID   uint32
	senderNodeID       uint32
}

type ACKHeader struct {
	originalPacketID uint32
}

type Data struct {
	finalDestID uint32
}

func (bh *BaseHeader) serialiseBaseHeader() ([]byte, error) {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4],bh.destNodeID)
	binary.LittleEndian.PutUint32(buf[4:8],bh.srcNodeID)
	binary.LittleEndian.PutUint32(buf[8:12],bh.packetID)
	buf[12] = bh.packetType
	buf[13] = bh.flags
	buf[14] = bh.hopCount
	buf[15] = bh.reserved 
	return buf, nil
}

func (bh *BaseHeader) deserialiseBaseHeader(buf []byte) error {
	if len(buf) < 16 {
		return fmt.Errorf("buffer too short for BaseHeader")
	}
	bh.destNodeID = binary.LittleEndian.Uint32(buf[0:4])
	bh.srcNodeID = binary.LittleEndian.Uint32(buf[4:8])
	bh.packetID = binary.LittleEndian.Uint32(buf[8:12])
	bh.packetType = buf[12]
	bh.flags = buf[13]
	bh.hopCount = buf[14]
	bh.reserved = buf[15]
	return nil
}

func (rreq *RREQHeader) serialiseRREQHeader() ([]byte, error) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], rreq.originNodeID)
    binary.LittleEndian.PutUint32(buf[4:8], rreq.RREQDestNodeID)
	return buf, nil

}

func (rreq *RREQHeader) deserialiseRREQHeader(buf []byte) error {
	if len(buf) < 8 {
		return fmt.Errorf("buffer too short for RREQHeader")
	}
	rreq.originNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rreq.RREQDestNodeID = binary.LittleEndian.Uint32(buf[4:8])
	return nil
}

func (rrep *RREPHeader) serialiseRREPHeader() ([]byte, error) {
	buf := make([]byte, 11)
	binary.LittleEndian.PutUint32(buf[0:4], rrep.originNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rrep.RREPDestNodeID)
	binary.LittleEndian.PutUint16(buf[8:10], rrep.lifetime)
	buf[10] = rrep.numHops
	return buf, nil
}

func (rrep *RREPHeader) deserialiseRREPHeader(buf []byte) error {
	if len(buf) < 11 {
		return fmt.Errorf("buffer too short for RREPHeader")
	}
	rrep.originNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rrep.RREPDestNodeID = binary.LittleEndian.Uint32(buf[4:8])
	rrep.lifetime = binary.LittleEndian.Uint16(buf[8:10])
	rrep.numHops = buf[10]
	return nil
}

func (rerr *RERRHeader) serialiseRERRHeader() ([]byte, error) {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], rerr.reporterNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rerr.brokenNodeID)
	binary.LittleEndian.PutUint32(buf[8:12], rerr.originalDestNodeID)
	binary.LittleEndian.PutUint32(buf[12:16], rerr.originalPacketID)
	binary.LittleEndian.PutUint32(buf[16:20], rerr.senderNodeID)
	return buf, nil
}

func (rerr *RERRHeader) deserialiseRERRHeader(buf []byte) error {
	if len(buf) < 20 {
		return fmt.Errorf("buffer too short for RERRHeader")
	}
	rerr.reporterNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rerr.brokenNodeID = binary.LittleEndian.Uint32(buf[4:8])
	rerr.originalDestNodeID = binary.LittleEndian.Uint32(buf[8:12])
	rerr.originalPacketID = binary.LittleEndian.Uint32(buf[12:16])
	rerr.senderNodeID = binary.LittleEndian.Uint32(buf[16:20])
	return nil
}

func (ack *ACKHeader) serialiseACKHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], ack.originalPacketID)
	return buf, nil
}

func (ack *ACKHeader) deserialiseACKHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for ACKHeader")
	}
	ack.originalPacketID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}

func (d *Data) serialiseDataHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], d.finalDestID)
	return buf, nil
}

func (d *Data) deserialiseDataHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for Data header")
	}
	d.finalDestID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}
