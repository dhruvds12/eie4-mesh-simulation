package packet

import (
	"encoding/binary"
	"fmt"
	"math/rand"
)

// Packet Types
const (
	PKT_DATA_BROADCAST uint8 = 0x00 //0 ==> never used but just for event bus logging
	PKT_RREQ           uint8 = 0x01 //1
	PKT_RREP           uint8 = 0x02 //2
	PKT_RERR           uint8 = 0x03 //3
	PKT_DATA           uint8 = 0x04 //4
	PKT_BROADCAST      uint8 = 0x05 //5
	PKT_BROADCAST_INFO uint8 = 0x06 //6
	PKT_ACK            uint8 = 0x07 //7
	PKT_UREQ           uint8 = 0x0F //15 // user lookup request
	PKT_UREP           uint8 = 0x10 //16 // user lookup reply
	PKT_UERR           uint8 = 0x11 //17 // user lookup error
	PKT_USER_MSG       uint8 = 0x12 //18 // user lookup error
	PKT_PUBKEY_REQ     uint8 = 0x13 //19 // unused in sim
	PKT_PUBKEY_RESP    uint8 = 0x14 //20 // unused in sim
	PKT_MOVE_USER_REQ  uint8 = 0x15 //21 //TODO need to implement
)

const (
	FROM_GATEWAY uint8 = 0x01
	TO_GATEWAY   uint8 = 0x02
	I_AM_GATEWAY uint8 = 0x03
	REQ_ACK      uint8 = 0x04
	ENC_MSG      uint8 = 0x10
)

const (
	MaxPacketSize = 255 // bytes – LoRa airtime optimiser

	BROADCAST_ADDR uint32 = 0xFFFFFFFF     // everyone hears
	BROADCAST_NH   uint32 = BROADCAST_ADDR // alias used by flood router

	MAX_HOPS = 3 // safety cap to avoid routing loops

	FLAG_ENCRYPTED = 0x80

	DATA_BROADCAST_LIMIT = 3
)

type BaseHeader struct {
	DestNodeID   uint32 // destination of the hop not the route
	PrevHopID    uint32
	OriginNodeID uint32
	PacketID     uint32
	PacketType   uint8
	Flags        uint8
	HopCount     uint8
	Reserved     uint8
}

type RREQHeader struct {
	// OriginNodeID   uint32
	RREQDestNodeID uint32
}

type RREPHeader struct {
	// OriginNodeID   uint32 // source of rreq
	RREPDestNodeID uint32 // dest of route
	Lifetime       uint16
	NumHops        uint8
}

type RERRHeader struct {
	ReporterNodeID     uint32 // the reporter node
	BrokenNodeID       uint32 // the broken nodeID
	OriginalDestNodeID uint32 // Original intended destination
	OriginalPacketID   uint32 // Original packetID that will have been overwritten in base header maintained here
	// SenderNodeID       uint32 // the original sender
}

type ACKHeader struct {
	OriginalPacketID uint32
}

type DataHeader struct {
	FinalDestID uint32
	// OriginNodeID uint32
}

type InfoHeader struct {
	// OriginNodeID uint32
	UserCount uint8
	Users     []uint32
}

type UREQHeader struct {
	// OriginNodeID uint32
	UREQUserID uint32 // requested userid
}

type UREPHeader struct {
	// OriginNodeID   uint32 // origin og UREQ
	UREPDestNodeID uint32 // destination node where target user is
	UREPUserID     uint32 // if of located user
	Lifetime       uint16
	NumHops        uint8
}

// Different to RERR used when the node was found but the user was not at the node
type UERRHeader struct {
	UserID uint32 // id of user that is not found
	NodeID uint32 // id of node that we thought the user was at
	// OriginNodeID     uint32
	OriginalPacketID uint32
}

type UserMsgHeader struct {
	FromUserID uint32
	ToUserID   uint32
	ToNodeID   uint32
	// OriginNodeID uint32
}

func ReadUint32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func (bh *BaseHeader) SerialiseBaseHeader() ([]byte, error) {
	buf := make([]byte, 20)
	binary.LittleEndian.PutUint32(buf[0:4], bh.DestNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], bh.PrevHopID)
	binary.LittleEndian.PutUint32(buf[8:12], bh.OriginNodeID)
	binary.LittleEndian.PutUint32(buf[12:16], bh.PacketID)
	buf[16] = bh.PacketType
	buf[17] = bh.Flags
	buf[18] = bh.HopCount
	buf[19] = bh.Reserved
	return buf, nil
}

func (bh *BaseHeader) DeserialiseBaseHeader(buf []byte) error {
	if len(buf) < 20 {
		return fmt.Errorf("buffer too short for BaseHeader")
	}
	bh.DestNodeID = binary.LittleEndian.Uint32(buf[0:4])
	bh.PrevHopID = binary.LittleEndian.Uint32(buf[4:8])
	bh.OriginNodeID = binary.LittleEndian.Uint32(buf[8:12])
	bh.PacketID = binary.LittleEndian.Uint32(buf[12:16])
	bh.PacketType = buf[16]
	bh.Flags = buf[17]
	bh.HopCount = buf[18]
	bh.Reserved = buf[19]
	return nil
}

func (rreq *RREQHeader) SerialiseRREQHeader() ([]byte, error) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf[0:4], rreq.RREQDestNodeID)
	return buf, nil

}

func (rreq *RREQHeader) DeserialiseRREQHeader(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("buffer too short for RREQHeader")
	}
	rreq.RREQDestNodeID = binary.LittleEndian.Uint32(buf[0:4])
	return nil
}

func (rrep *RREPHeader) SerialiseRREPHeader() ([]byte, error) {
	buf := make([]byte, 7)
	binary.LittleEndian.PutUint32(buf[0:4], rrep.RREPDestNodeID)
	binary.LittleEndian.PutUint16(buf[4:6], rrep.Lifetime)
	buf[6] = rrep.NumHops
	return buf, nil
}

func (rrep *RREPHeader) DeserialiseRREPHeader(buf []byte) error {
	if len(buf) < 7 {
		return fmt.Errorf("buffer too short for RREPHeader")
	}
	rrep.RREPDestNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rrep.Lifetime = binary.LittleEndian.Uint16(buf[4:6])
	rrep.NumHops = buf[6]
	return nil
}

func (rerr *RERRHeader) SerialiseRERRHeader() ([]byte, error) {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], rerr.ReporterNodeID)
	binary.LittleEndian.PutUint32(buf[4:8], rerr.BrokenNodeID)
	binary.LittleEndian.PutUint32(buf[8:12], rerr.OriginalDestNodeID)
	binary.LittleEndian.PutUint32(buf[12:16], rerr.OriginalPacketID)
	return buf, nil
}

func (rerr *RERRHeader) DeserialiseRERRHeader(buf []byte) error {
	if len(buf) < 16 {
		return fmt.Errorf("buffer too short for RERRHeader")
	}
	rerr.ReporterNodeID = binary.LittleEndian.Uint32(buf[0:4])
	rerr.BrokenNodeID = binary.LittleEndian.Uint32(buf[4:8])
	rerr.OriginalDestNodeID = binary.LittleEndian.Uint32(buf[8:12])
	rerr.OriginalPacketID = binary.LittleEndian.Uint32(buf[12:16])
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
	//  1 B count + 4 B×len(Users)
	total := 1 + int(i.UserCount)*4
	buf := make([]byte, total)

	buf[0] = i.UserCount
	offset := 1
	for idx, uid := range i.Users {
		binary.LittleEndian.PutUint32(buf[offset+idx*4:offset+idx*4+4], uid)
	}
	return buf, nil
}

func (i *InfoHeader) DeserialiseInfoHeader(buf []byte) error {
	if len(buf) < 1 {
		return fmt.Errorf("buffer too short for InfoHeader")
	}
	i.UserCount = buf[0]

	expected := 1 + int(i.UserCount)*4
	if len(buf) < expected {
		return fmt.Errorf("buffer too short for %d users: need %d B got %d B",
			i.UserCount, expected, len(buf))
	}

	i.Users = make([]uint32, i.UserCount)
	offset := 1
	for j := 0; j < int(i.UserCount); j++ {
		i.Users[j] = binary.LittleEndian.Uint32(buf[offset+j*4 : offset+j*4+4])
	}
	return nil
}

func createPacketID() uint32 {
	return uint32(rand.Int31())
}

func chooseID(ids ...uint32) uint32 {
	if len(ids) > 0 {
		return ids[0]
	}
	return createPacketID()
}

func CreateDataPacket(originNodeID, srcID, destID, nextHopID uint32, numHops uint8, payload []byte, flags uint8, packetID ...uint32) ([]byte, uint32, error) {

	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID:   nextHopID,
		PrevHopID:    srcID,
		OriginNodeID: originNodeID,
		PacketID:     pid,
		PacketType:   PKT_DATA,
		Flags:        flags,
		HopCount:     numHops,
		Reserved:     0x0,
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

	pid := chooseID(packetID...)
	bh := BaseHeader{
		DestNodeID:   BROADCAST_ADDR,
		PrevHopID:    srcID,
		OriginNodeID: orginNode,
		PacketID:     pid,
		PacketType:   PKT_RREQ,
		Flags:        0x0,
		HopCount:     numHops,
		Reserved:     0x0,
	}

	rreq := RREQHeader{
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

func CreateRREPPacket(srcID, destRouteID, nextHopID, orginNode uint32, lifetime uint16, numHops, rrepNumHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID:   nextHopID,
		PrevHopID:    srcID,
		OriginNodeID: orginNode, // node that wanted the route
		PacketID:     pid,
		PacketType:   PKT_RREP,
		Flags:        0x0,
		HopCount:     numHops,
		Reserved:     0x0,
	}

	rrep := RREPHeader{
		RREPDestNodeID: destRouteID, // destination of the route
		Lifetime:       lifetime,
		NumHops:        rrepNumHops,
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

func CreateRERRPacket(srcID, nextHopID, reporterNodeID, brokenNodeID, originalDestNodeID, originalPacketID, originNodeID uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {

	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID:   nextHopID,
		PrevHopID:    srcID,
		OriginNodeID: originNodeID,
		PacketID:     pid,
		PacketType:   PKT_RERR,
		Flags:        0x0,
		HopCount:     numHops,
		Reserved:     0x0,
	}

	rerr := RERRHeader{
		ReporterNodeID:     reporterNodeID,
		BrokenNodeID:       brokenNodeID,
		OriginalDestNodeID: originalDestNodeID,
		OriginalPacketID:   originalPacketID,
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

	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID: nextHopID,
		PrevHopID:  srcID,
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
		return nil, 0, fmt.Errorf("error serailising ack")
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

func CreateBroadcastInfoPacket(
	srcID, originNode uint32,
	userIDs []uint32,
	numHops uint8,
	packetID ...uint32,
) ([]byte, uint32, error) {
	// pick or reuse packetID…
	pid := chooseID(packetID...)

	bh := BaseHeader{
		DestNodeID:   BROADCAST_ADDR,
		PrevHopID:    srcID,
		OriginNodeID: originNode,
		PacketID:     pid,
		PacketType:   PKT_BROADCAST_INFO,
		Flags:        0x0,
		HopCount:     numHops,
		Reserved:     0x0,
	}

	ih := InfoHeader{
		UserCount: uint8(len(userIDs)),
		Users:     userIDs,
	}

	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader: %w", err)
	}

	ihBytes, err := ih.SerialiseInfoHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising InfoHeader: %w", err)
	}

	totalLength := len(bhBytes) + len(ihBytes)
	if totalLength > MaxPacketSize {
		return nil, 0, fmt.Errorf("BroadcastInfo packet too big (%d B)", totalLength)
	}

	pkt := make([]byte, totalLength)
	copy(pkt[0:], bhBytes)
	copy(pkt[len(bhBytes):], ihBytes)

	return pkt, pid, nil
}

// DeserializeRREQPacket reads a RREQ packet from buf.
// It returns the deserialized BaseHeader and RREQHeader.
func DeserialiseRREQPacket(buf []byte) (bh BaseHeader, rreq RREQHeader, err error) {
	offset := 0

	if len(buf) < offset+20 {
		return bh, rreq, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

	if len(buf) < offset+4 {
		return bh, rreq, fmt.Errorf("buffer too short for RREQHeader")
	}
	err = rreq.DeserialiseRREQHeader(buf[offset : offset+4])
	if err != nil {
		return
	}
	offset += 4

	return bh, rreq, nil
}

// DeserializeRREPPacket reads a RREP packet from buf.
// It returns the deserialized BaseHeader and RREPHeader.
func DeserialiseRREPPacket(buf []byte) (bh BaseHeader, rrep RREPHeader, err error) {
	offset := 0

	if len(buf) < offset+20 {
		return bh, rrep, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

	if len(buf) < offset+7 {
		return bh, rrep, fmt.Errorf("buffer too short for RREPHeader")
	}
	err = rrep.DeserialiseRREPHeader(buf[offset : offset+7])
	if err != nil {
		return
	}
	offset += 7

	return bh, rrep, nil
}

// DeserializeRERRPacket reads a RERR packet from buf.
// It returns the deserialized BaseHeader and RERRHeader.
func DeserialiseRERRPacket(buf []byte) (bh BaseHeader, rerr RERRHeader, err error) {
	offset := 0

	if len(buf) < offset+20 {
		return bh, rerr, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

	if len(buf) < offset+16 {
		return bh, rerr, fmt.Errorf("buffer too short for RERRHeader")
	}
	err = rerr.DeserialiseRERRHeader(buf[offset : offset+16])
	if err != nil {
		return
	}
	offset += 16

	return bh, rerr, nil
}

// DeserializeACKPacket reads an ACK packet from buf.
// It returns the deserialized BaseHeader and ACKHeader.
func DeserialiseACKPacket(buf []byte) (bh BaseHeader, ack ACKHeader, err error) {
	offset := 0

	if len(buf) < offset+20 {
		return bh, ack, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

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

	if len(buf) < offset+20 {
		return bh, dh, nil, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

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

	if len(buf) < offset+20 {
		return bh, ih, fmt.Errorf("buffer too short for BaseHeader")
	}
	err = bh.DeserialiseBaseHeader(buf[offset : offset+20])
	if err != nil {
		return
	}
	offset += 20

	// after reading BaseHeader
	if len(buf) < offset+1 {
		return bh, ih, fmt.Errorf("buffer too short for InfoHeader count")
	}
	count := int(buf[offset])
	needed := 1 + count*4
	if len(buf) < offset+needed {
		return bh, ih, fmt.Errorf("buffer too short for %d users: need %d bytes, got %d", count, offset+needed, len(buf))
	}
	err = ih.DeserialiseInfoHeader(buf[offset : offset+needed])

	if err != nil {
		return
	}

	return bh, ih, nil
}

// CreateUREQPacket constructs a UREQ (user lookup) packet
func CreateUREQPacket(srcID, originNode, targetUser uint32, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {
	pid := chooseID(packetID...)
	bh := BaseHeader{
		DestNodeID:   BROADCAST_ADDR,
		PrevHopID:    srcID,
		OriginNodeID: originNode,
		PacketID:     pid,
		PacketType:   PKT_UREQ,
		Flags:        0x0,
		HopCount:     numHops,
		Reserved:     0x0,
	}
	h := UREQHeader{
		UREQUserID: targetUser,
	}

	bhb, _ := bh.SerialiseBaseHeader()
	hb := make([]byte, 4)
	binary.LittleEndian.PutUint32(hb[0:4], h.UREQUserID)

	buf := append(bhb, hb...)
	return buf, pid, nil
}

// DeserialiseUREQPacket unpacks a UREQ packet
func DeserialiseUREQPacket(buf []byte) (BaseHeader, UREQHeader, error) {
	var bh BaseHeader
	if err := bh.DeserialiseBaseHeader(buf[:20]); err != nil {
		return bh, UREQHeader{}, err
	}
	ofs := 20
	if len(buf) < ofs+4 {
		return bh, UREQHeader{}, fmt.Errorf("buffer too short for UREQHeader")
	}
	h := UREQHeader{
		UREQUserID: binary.LittleEndian.Uint32(buf[ofs : ofs+4]),
	}
	return bh, h, nil
}

// CreateUREPPacket constructs a UREP (user lookup reply) packet
func CreateUREPPacket(srcID, destID, originNode, UREPDestNodeID, userID uint32, lifetime uint16, numHops uint8, packetID ...uint32) ([]byte, uint32, error) {
	pid := chooseID(packetID...)
	bh := BaseHeader{
		DestNodeID:   destID,
		PrevHopID:    srcID,
		OriginNodeID: originNode,
		PacketID:     pid,
		PacketType:   PKT_UREP,
		Flags:        0,
		HopCount:     numHops,
		Reserved:     0,
	}
	h := UREPHeader{
		UREPDestNodeID: UREPDestNodeID,
		UREPUserID:     userID,
		Lifetime:       lifetime,
		NumHops:        numHops,
	}

	bhb, _ := bh.SerialiseBaseHeader()
	hb := make([]byte, 11)
	binary.LittleEndian.PutUint32(hb[0:4], h.UREPDestNodeID)
	binary.LittleEndian.PutUint32(hb[4:8], h.UREPUserID)
	binary.LittleEndian.PutUint16(hb[8:10], h.Lifetime)
	hb[10] = h.NumHops

	buf := append(bhb, hb...)
	return buf, pid, nil
}

// DeserialiseUREPPacket unpacks a UREP packet
func DeserialiseUREPPacket(buf []byte) (BaseHeader, UREPHeader, error) {
	var bh BaseHeader
	if err := bh.DeserialiseBaseHeader(buf[:20]); err != nil {
		return bh, UREPHeader{}, err
	}
	ofs := 20
	if len(buf) < ofs+11 {
		return bh, UREPHeader{}, fmt.Errorf("buffer too short for UREPHeader")
	}
	h := UREPHeader{
		UREPDestNodeID: binary.LittleEndian.Uint32(buf[ofs+0 : ofs+4]),
		UREPUserID:     binary.LittleEndian.Uint32(buf[ofs+4 : ofs+8]),
		Lifetime:       binary.LittleEndian.Uint16(buf[ofs+8 : ofs+10]),
		NumHops:        buf[ofs+10],
	}
	return bh, h, nil
}

// CreateUERRPacket constructs a UERR (user lookup error) packet.
// Now includes UERRUserID, UERRNodeID, OriginNode, and OriginalPacketID.
func CreateUERRPacket(
	srcID, destNodeID,
	uerrUserID, uerrNodeID,
	originNodeID, originalPacketID uint32,
	packetID ...uint32,
) ([]byte, uint32, error) {
	pid := chooseID(packetID...)
	bh := BaseHeader{
		DestNodeID:   destNodeID,
		PrevHopID:    srcID,
		OriginNodeID: originNodeID,
		PacketID:     pid,
		PacketType:   PKT_UERR,
		Flags:        0,
		HopCount:     0,
		Reserved:     0,
	}
	h := UERRHeader{
		UserID:           uerrUserID,
		NodeID:           uerrNodeID,
		OriginalPacketID: originalPacketID,
	}

	// serialize base header
	bhb, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader: %w", err)
	}

	// serialize extended header (4×4 B)
	hb := make([]byte, 12)
	binary.LittleEndian.PutUint32(hb[0:4], h.UserID)
	binary.LittleEndian.PutUint32(hb[4:8], h.NodeID)
	binary.LittleEndian.PutUint32(hb[8:12], h.OriginalPacketID)

	// combine and return
	buf := append(bhb, hb...)
	return buf, pid, nil
}

// DeserialiseUERRPacket unpacks a UERR packet into BaseHeader + UERRHeader.
func DeserialiseUERRPacket(
	buf []byte,
) (BaseHeader, UERRHeader, error) {
	var bh BaseHeader
	// first 20 = BaseHeader
	if len(buf) < 20 {
		return bh, UERRHeader{}, fmt.Errorf("buffer too short for BaseHeader")
	}
	if err := bh.DeserialiseBaseHeader(buf[:20]); err != nil {
		return bh, UERRHeader{}, err
	}

	// next 12 = UERRHeader
	const hdrLen = 20
	if len(buf) < 12+hdrLen {
		return bh, UERRHeader{}, fmt.Errorf(
			"buffer too short for UERRHeader: need %d, got %d",
			12+hdrLen, len(buf),
		)
	}
	ofs := hdrLen
	h := UERRHeader{
		UserID:           binary.LittleEndian.Uint32(buf[ofs+0 : ofs+4]),
		NodeID:           binary.LittleEndian.Uint32(buf[ofs+4 : ofs+8]),
		OriginalPacketID: binary.LittleEndian.Uint32(buf[ofs+8 : ofs+12]),
	}
	return bh, h, nil
}

// SerialiseUSERMessageHeader writes the USERMessageHeader into a 16-byte slice
func (h *UserMsgHeader) SerialiseUSERMessageHeader() ([]byte, error) {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[0:4], h.FromUserID)
	binary.LittleEndian.PutUint32(buf[4:8], h.ToUserID)
	binary.LittleEndian.PutUint32(buf[8:12], h.ToNodeID)
	return buf, nil
}

// DeserialiseUSERMessageHeader parses a 16-byte slice into USERMessageHeader
func (h *UserMsgHeader) DeserialiseUSERMessageHeader(buf []byte) error {
	if len(buf) < 12 {
		return fmt.Errorf("buffer too short for USERMessageHeader: need 12, got %d", len(buf))
	}
	h.FromUserID = binary.LittleEndian.Uint32(buf[0:4])
	h.ToUserID = binary.LittleEndian.Uint32(buf[4:8])
	h.ToNodeID = binary.LittleEndian.Uint32(buf[8:12])
	return nil
}

// CreateUSERMessagePacket builds a full packet containing BaseHeader + USERMessageHeader + payload
func CreateUSERMessagePacket(
	originNodeID, srcID, senderUserID, destUserID, destNodeID, nextHopID uint32,
	hopCount uint8,
	payload []byte,
	flags uint8,
	packetID ...uint32,
) ([]byte, uint32, error) {
	// pick or generate a packet ID
	var pid uint32
	if len(packetID) > 0 {
		pid = packetID[0]
	} else {
		pid = createPacketID()
	}

	// construct BaseHeader (dest = nextHop)
	bh := BaseHeader{
		DestNodeID:   nextHopID,
		PrevHopID:    srcID,
		OriginNodeID: originNodeID,
		PacketID:     pid,
		PacketType:   PKT_USER_MSG,
		Flags:        flags,
		HopCount:     hopCount,
		Reserved:     0x0,
	}

	// construct USERMessageHeader
	mh := UserMsgHeader{
		FromUserID: senderUserID,
		ToUserID:   destUserID,
		ToNodeID:   destNodeID,
	}

	// serialize headers
	bhBytes, err := bh.SerialiseBaseHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising BaseHeader: %w", err)
	}
	mhBytes, err := mh.SerialiseUSERMessageHeader()
	if err != nil {
		return nil, 0, fmt.Errorf("error serialising USERMessageHeader: %w", err)
	}

	// determine packet length, truncating payload if needed
	total := len(bhBytes) + len(mhBytes) + len(payload)
	if total > MaxPacketSize {
		allowed := MaxPacketSize - (len(bhBytes) + len(mhBytes))
		payload = payload[:allowed]
		total = MaxPacketSize
	}

	// assemble packet
	buf := make([]byte, total)
	ofs := 0
	copy(buf[ofs:], bhBytes)
	ofs += len(bhBytes)
	copy(buf[ofs:], mhBytes)
	ofs += len(mhBytes)
	copy(buf[ofs:], payload)

	return buf, pid, nil
}

// DeserialiseUSERMessagePacket splits a buffer into BaseHeader, USERMessageHeader, and payload
func DeserialiseUSERMessagePacket(
	buf []byte,
) (BaseHeader, UserMsgHeader, []byte, error) {
	var bh BaseHeader
	var mh UserMsgHeader

	// BaseHeader is 20 bytes
	if len(buf) < 20 {
		return bh, mh, nil, fmt.Errorf("buffer too short for BaseHeader: need 20, got %d", len(buf))
	}
	if err := bh.DeserialiseBaseHeader(buf[0:20]); err != nil {
		return bh, mh, nil, err
	}

	// USERMessageHeader is next 12 bytes
	ofs := 20
	if len(buf) < ofs+12 {
		return bh, mh, nil, fmt.Errorf("buffer too short for USERMessageHeader: need %d, got %d", ofs+12, len(buf))
	}
	if err := mh.DeserialiseUSERMessageHeader(buf[ofs : ofs+12]); err != nil {
		return bh, mh, nil, err
	}

	// remainder is payload
	payload := buf[ofs+12:]
	return bh, mh, payload, nil
}
