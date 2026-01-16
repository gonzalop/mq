package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// AuthPacket represents an MQTT v5.0 AUTH control packet.
//
// The AUTH packet is used for extended authentication exchanges between
// client and server. It enables challenge/response authentication mechanisms
// such as SCRAM, OAuth, Kerberos, etc.
type AuthPacket struct {
	ReasonCode uint8       // Authentication reason code
	Properties *Properties // Authentication properties (method, data, etc.)
	Version    uint8       // Protocol version (must be 5)
}

// AUTH reason codes
const (
	AuthReasonSuccess        uint8 = 0x00 // Authentication successful
	AuthReasonContinue       uint8 = 0x18 // Continue authentication
	AuthReasonReauthenticate uint8 = 0x19 // Re-authenticate
)

// Type returns the packet type (AUTH = 15).
func (p *AuthPacket) Type() uint8 {
	return AUTH
}

// Encode serializes the AUTH packet to bytes.

// WriteTo writes the AUTH packet to the writer.
func (p *AuthPacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0
	// Reason Code (1 byte) + Properties
	if p.Version >= 5 {
		propsBytes = encodeProperties(p.Properties)
		propsLen = len(propsBytes)
	}

	variableHeaderLen := 1 + propsLen // ReasonCode + Props

	// 2. Write Fixed Header
	header := &FixedHeader{
		PacketType:      AUTH,
		Flags:           0,
		RemainingLength: variableHeaderLen,
	}

	// Write Fixed Header
	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}

	// 3. Write Variable Header
	// Reason Code
	if err := binary.Write(w, binary.BigEndian, p.ReasonCode); err != nil {
		return total, err
	}
	total++

	// Properties
	if p.Version >= 5 {
		n, err := w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// DecodeAuth decodes an AUTH packet from the buffer.
func DecodeAuth(buf []byte, version uint8) (*AuthPacket, error) {
	if version < 5 {
		return nil, fmt.Errorf("AUTH packet is only valid for MQTT v5.0")
	}

	if len(buf) < 1 {
		return nil, fmt.Errorf("buffer too short for AUTH packet")
	}

	pkt := &AuthPacket{
		Version: version,
	}

	offset := 0

	// Reason Code (1 byte)
	pkt.ReasonCode = buf[offset]
	offset++

	// Properties
	if offset < len(buf) {
		props, _, err := decodeProperties(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
	}

	return pkt, nil
}
