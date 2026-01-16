package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// SubscribePacket represents an MQTT SUBSCRIBE control packet.
type SubscribePacket struct {
	PacketID uint16
	Topics   []string
	QoS      []uint8 // QoS level for each topic

	// MQTT v5.0 Subscription Options
	// These slices must match the length of Topics/QoS if provided.
	// If nil/empty, defaults (false/0) are used.
	NoLocal           []bool
	RetainAsPublished []bool
	RetainHandling    []uint8 // 0=Send, 1=SendIfNew, 2=DoNotSend

	// MQTT v5.0 fields
	Properties *Properties // v5.0
	Version    uint8       // 4 or 5
}

// Type returns the packet type.
func (p *SubscribePacket) Type() uint8 {
	return SUBSCRIBE
}

// Encode serializes the SUBSCRIBE packet to bytes.

// WriteTo writes the SUBSCRIBE packet to the writer.
func (p *SubscribePacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var packetIDBytes [2]byte
	var propsBytes []byte
	var propsLen int

	// MQTT v5.0 Properties
	if p.Version >= 5 {
		propsBytes = encodeProperties(p.Properties)
		propsLen = len(propsBytes)
	}

	variableHeaderLen := 2 + propsLen // PacketID + Props

	// 2. Calculate Payload Length
	var payloadLen int
	var topicBytesList [][]byte

	for _, topic := range p.Topics {
		tb := encodeString(topic)
		topicBytesList = append(topicBytesList, tb)
		payloadLen += len(tb) + 1 // Topic + OptionsByte
	}

	// 3. Write Fixed Header
	// SUBSCRIBE has fixed header flags = 0x02 (bit 1 set)
	remainingLength := variableHeaderLen + payloadLen
	header := &FixedHeader{
		PacketType:      SUBSCRIBE,
		Flags:           0x02,
		RemainingLength: remainingLength,
	}

	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}
	var n int

	// 4. Write Variable Header
	// Packet ID
	binary.BigEndian.PutUint16(packetIDBytes[:], p.PacketID)
	n, err = w.Write(packetIDBytes[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Properties (v5.0)
	if p.Version >= 5 {
		n, err = w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// 5. Write Payload
	for i, tb := range topicBytesList {
		// Topic
		n, err = w.Write(tb)
		total += int64(n)
		if err != nil {
			return total, err
		}

		// Options Byte
		optionsByte := byte(0)

		// QoS (Bits 0-1)
		qos := uint8(QoS0)
		if i < len(p.QoS) {
			qos = p.QoS[i]
		}
		optionsByte |= (qos & 0x03)

		if p.Version >= 5 {
			// No Local (Bit 2)
			if i < len(p.NoLocal) && p.NoLocal[i] {
				optionsByte |= (1 << 2)
			}

			// Retain As Published (Bit 3)
			if i < len(p.RetainAsPublished) && p.RetainAsPublished[i] {
				optionsByte |= (1 << 3)
			}

			// Retain Handling (Bits 4-5)
			if i < len(p.RetainHandling) {
				rh := p.RetainHandling[i]
				optionsByte |= ((rh & 0x03) << 4)
			}
		}

		if err := binary.Write(w, binary.BigEndian, optionsByte); err != nil {
			return total, err
		}
		total++
	}

	return total, nil
}

// DecodeSubscribe decodes a SUBSCRIBE packet from the buffer.
func DecodeSubscribe(buf []byte, version uint8) (*SubscribePacket, error) {
	if len(buf) < 2 {
		return nil, fmt.Errorf("buffer too short for SUBSCRIBE packet")
	}

	pkt := &SubscribePacket{
		Version: version,
	}

	offset := 0

	// Packet ID
	pkt.PacketID = binary.BigEndian.Uint16(buf[offset : offset+2])
	offset += 2

	// v5.0 Properties
	if version >= 5 {
		if offset >= len(buf) {
			return nil, fmt.Errorf("buffer too short for properties length")
		}
		props, n, err := decodeProperties(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
		offset += n
	}

	// Topic filters with Subscription Options
	for offset < len(buf) {
		topic, n, err := decodeString(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode topic filter: %w", err)
		}
		offset += n

		if offset >= len(buf) {
			return nil, fmt.Errorf("buffer too short for options byte")
		}

		opts := buf[offset]
		offset++

		// QoS (Bits 0-1)
		pkt.Topics = append(pkt.Topics, topic)
		pkt.QoS = append(pkt.QoS, opts&0x03)

		if version >= 5 {
			// No Local (Bit 2)
			pkt.NoLocal = append(pkt.NoLocal, (opts&(1<<2)) != 0)

			// Retain As Published (Bit 3)
			pkt.RetainAsPublished = append(pkt.RetainAsPublished, (opts&(1<<3)) != 0)

			// Retain Handling (Bits 4-5)
			pkt.RetainHandling = append(pkt.RetainHandling, (opts>>4)&0x03)
		}
	}

	return pkt, nil
}
