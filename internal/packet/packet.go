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

const BROADCAST_ADDR uint32 = 0xFFFFFFFF

const MAX_HOPS = 3

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

type InfoHeader struct {
	OriginNodeID uint32
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

func (i *InfoHeader) SerialiseInfoHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], i.OriginNodeID)
	return buf, nil
}

func (i *InfoHeader) DeserialiseInfoHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for Data header")
	}
	i.OriginNodeID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}

func createPacketID() uint32 {
	return uint32(rand.Int31())
}

func CreateDataPacket(srcID, destID, nextHopID uint32, numHops uint8, payload []byte, packetID ...uint32) ([]byte, uint32, error) {

	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: nextHopID,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_DATA,
		Flags:      0x0,
		HopCount:   numHops,
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

	return packetBuffer, pid, nil
}

func CreateRREQPacket(srcID, destID, orginNode uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: BROADCAST_ADDR,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_RREQ,
		Flags:      0x0,
		HopCount:   numHops,
		Reserved:   0x0,
	}

	rreq := RREQHeader{
		OriginNodeID:   orginNode,
		RREQDestNodeID: destID,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	rreqBytes, err := rreq.SerialiseRREQHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serailising RREPHeader")
	}

	totalLength := len(bhBytes) + len(rreqBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("error RREQ packet too big")
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], rreqBytes)

	return packetBuffer, pid, nil
}

func CreateRREPPacket(srcID, destID, nextHopID, orginNode uint32, lifetime uint16, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: nextHopID,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_RREP,
		Flags:      0x0,
		HopCount:   numHops,
		Reserved:   0x0,
	}

	rrep := RREPHeader{
		OriginNodeID:   orginNode,
		RREPDestNodeID: destID,
		Lifetime:       lifetime,
		NumHops:        numHops,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	rrepBytes, err := rrep.SerialiseRREPHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serailising RREPHeader")
	}

	totalLength := len(bhBytes) + len(rrepBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("error RREP packet too big")
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], rrepBytes)

	return packetBuffer, pid, nil
}

func CreateRERRPacket(srcID, nextHopID, reporterNodeID, brokenNodeID, originalDestNodeID, originalPacketID, senderNodeID uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: nextHopID,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_RERR,
		Flags:      0x0,
		HopCount:   numHops,
		Reserved:   0x0,
	}

	rerr := RERRHeader{
		ReporterNodeID:     reporterNodeID,
		BrokenNodeID:       brokenNodeID,
		OriginalDestNodeID: originalDestNodeID,
		OriginalPacketID:   originalPacketID,
		SenderNodeID:       senderNodeID,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	rerrBytes, err := rerr.SerialiseRERRHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serailising RREPHeader")
	}

	totalLength := len(bhBytes) + len(rerrBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("error RERR packet too big")
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], rerrBytes)

	return packetBuffer, pid, nil
}

func CreateACKPacket(srcID, destID, nextHopID, originalPacketID uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: nextHopID,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_ACK,
		Flags:      0x0,
		HopCount:   numHops,
		Reserved:   0x0,
	}

	ack := ACKHeader{
		OriginalPacketID: originalPacketID,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	ackBytes, err := ack.SerialiseACKHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serailising RREPHeader")
	}

	totalLength := len(bhBytes) + len(ackBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("error ACK packet too big")
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], ackBytes)

	return packetBuffer, pid, nil
}

func CreateBroadcastInfoPacket(srcID, originNode uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {
	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	bh := BaseHeader{
		DestNodeID: BROADCAST_ADDR,
		SrcNodeID:  srcID,
		PacketID:   pid,
		PacketType: PKT_BROADCAST_INFO,
		Flags:      0x0,
		HopCount:   numHops,
		Reserved:   0x0,
	}

	ih := InfoHeader{
		OriginNodeID: originNode,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader")
	}

	ihBytes, err := ih.SerialiseInfoHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising InfoHeader")
	}

	totalLength := len(bhBytes) + len(ihBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("error BroadcastInfo packet too big")
	}

	packetBuffer := make([]byte, totalLength)
	offset := 0

	copy(packetBuffer[offset:], bhBytes)
	offset += len(bhBytes)

	copy(packetBuffer[offset:], ihBytes)

	return packetBuffer, pid, nil
}

// DeserializeRREQPacket reads a RREQ packet from buf.
// It returns the deserialized BaseHeader and RREQHeader.
func DeserialiseRREQPacket(buf []byte) (bh BaseHeader, rreq RREQHeader, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, rreq, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+8 {
		return bh, rreq, fmt.Errorf("buffer too short for RREQHeader")
	}
	err = rreq.DeserialiseRREQHeader(buf[offset : offset+8])
	if err != nil {
		return
	}
	offset += 8

	return bh, rreq, nil
}

// DeserializeRREPPacket reads a RREP packet from buf.
// It returns the deserialized BaseHeader and RREPHeader.
func DeserialiseRREPPacket(buf []byte) (bh BaseHeader, rrep RREPHeader, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, rrep, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+11 {
		return bh, rrep, fmt.Errorf("buffer too short for RREPHeader")
	}
	err = rrep.DeserialiseRREPHeader(buf[offset : offset+11])
	if err != nil {
		return
	}
	offset += 11

	return bh, rrep, nil
}

// DeserializeRERRPacket reads a RERR packet from buf.
// It returns the deserialized BaseHeader and RERRHeader.
func DeserialiseRERRPacket(buf []byte) (bh BaseHeader, rerr RERRHeader, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, rerr, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+20 {
		return bh, rerr, fmt.Errorf("buffer too short for RERRHeader")
	}
	err = rerr.DeserialiseRERRHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

	return bh, rerr, nil
}

// DeserializeACKPacket reads an ACK packet from buf.
// It returns the deserialized BaseHeader and ACKHeader.
func DeserialiseACKPacket(buf []byte) (bh BaseHeader, ack ACKHeader, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, ack, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+4 {
		return bh, ack, fmt.Errorf("buffer too short for ACKHeader")
	}
	err = ack.DeserialiseACKHeader(buf[offset : offset+4])
	if err != nil {
		return
	}
	offset += 4

	return bh, ack, nil
}

// DeserializeDataPacket reads a Data packet from buf.
// It returns the deserialized BaseHeader, DataHeader, and the remaining payload.
func DeserialiseDataPacket(buf []byte) (bh BaseHeader, dh DataHeader, payload []byte, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, dh, nil, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+4 {
		return bh, dh, nil, fmt.Errorf("buffer too short for DataHeader")
	}
	err = dh.DeserialiseDataHeader(buf[offset : offset+4])
	if err != nil {
		return
	}
	offset += 4

	// The remainder is considered the payload.
	if len(buf) > offset {
		payload = buf[offset:]
	}
	return bh, dh, payload, nil
}

func DeserialiseInfoPacket(buf []byte) (bh BaseHeader, ih InfoHeader, err error) {
	offset := 0

	if len(buf) < offset+16 {
		return bh, ih, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	if len(buf) < offset+4 {
		return bh, ih, fmt.Errorf("buffer too short for InfoHeader")
	}
	err = ih.DeserialiseInfoHeader(buf[offset : offset+4])
	if err != nil {
		return
	}

	return bh, ih, nil
}
