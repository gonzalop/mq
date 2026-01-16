package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

// ConnectPacket represents an MQTT CONNECT control packet.
type ConnectPacket struct {
	// Protocol name (should be "MQTT" for v3.1.1)
	ProtocolName string

	// Protocol level (4 for v3.1.1, 5 for v5.0)
	ProtocolLevel uint8

	// Connect flags
	CleanSession bool
	WillFlag     bool
	WillQoS      uint8
	WillRetain   bool
	PasswordFlag bool
	UsernameFlag bool

	// Keep alive timer in seconds
	KeepAlive uint16

	// Payload
	ClientID string

	// Will fields (only used if WillFlag is true)
	WillTopic      string
	WillMessage    []byte
	WillProperties *Properties // MQTT v5.0

	// Credentials (only used if respective flags are true)
	Username string
	Password string

	// MQTT v5.0 fields
	Properties *Properties
}

// Type returns the packet type.
func (p *ConnectPacket) Type() uint8 {
	return CONNECT
}

// Encode serializes the CONNECT packet to bytes.

// WriteTo writes the CONNECT packet to the writer.
func (p *ConnectPacket) WriteTo(w io.Writer) (int64, error) {
	var total int64

	// 1. Calculate Variable Header length
	var protocolNameBytes []byte
	var protocolNameLen int
	var connectFlags uint8
	var keepAliveBytes [2]byte
	var propsBytes []byte
	var propsLen int

	protocolNameBytes = encodeString(p.ProtocolName)
	protocolNameLen = len(protocolNameBytes)

	// Flags
	if p.CleanSession {
		connectFlags |= 0x02
	}
	if p.WillFlag {
		connectFlags |= 0x04
		connectFlags |= (p.WillQoS & 0x03) << 3
		if p.WillRetain {
			connectFlags |= 0x20
		}
	}
	if p.PasswordFlag {
		connectFlags |= 0x40
	}
	if p.UsernameFlag {
		connectFlags |= 0x80
	}

	// Properties (v5.0 only)
	if p.ProtocolLevel >= 5 {
		propsBytes = encodeProperties(p.Properties)
		propsLen = len(propsBytes)
	}

	variableHeaderLen := protocolNameLen + 1 + 1 + 2 // Name + Level + Flags + KeepAlive
	if p.ProtocolLevel >= 5 {
		variableHeaderLen += propsLen
	}

	// 2. Calculate Payload Length
	var clientIDBytes []byte
	var willPropsBytes []byte
	var willTopicBytes []byte
	var willMsgBytes []byte
	var usernameBytes []byte
	var passwordBytes []byte

	clientIDBytes = encodeString(p.ClientID)
	payloadLen := len(clientIDBytes)

	if p.WillFlag {
		if p.ProtocolLevel >= 5 {
			willPropsBytes = encodeProperties(p.WillProperties)
			payloadLen += len(willPropsBytes)
		}
		willTopicBytes = encodeString(p.WillTopic)
		willMsgBytes = encodeBinary(p.WillMessage)
		payloadLen += len(willTopicBytes) + len(willMsgBytes)
	}

	if p.UsernameFlag {
		usernameBytes = encodeString(p.Username)
		payloadLen += len(usernameBytes)
	}

	if p.PasswordFlag {
		passwordBytes = encodeString(p.Password)
		payloadLen += len(passwordBytes)
	}

	// 3. Write Fixed Header
	remainingLength := variableHeaderLen + payloadLen
	header := &FixedHeader{
		PacketType:      CONNECT,
		Flags:           0,
		RemainingLength: remainingLength,
	}

	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}
	var n int

	// 4. Write Variable Header
	// Protocol Name
	n, err = w.Write(protocolNameBytes)
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Protocol Level
	if err := binary.Write(w, binary.BigEndian, p.ProtocolLevel); err != nil {
		return total, err
	}
	total++

	// Flags
	if err := binary.Write(w, binary.BigEndian, connectFlags); err != nil {
		return total, err
	}
	total++

	// Keep Alive
	binary.BigEndian.PutUint16(keepAliveBytes[:], p.KeepAlive)
	n, err = w.Write(keepAliveBytes[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Properties (v5.0)
	if p.ProtocolLevel >= 5 {
		n, err = w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// 5. Write Payload
	// Client ID
	n, err = w.Write(clientIDBytes)
	total += int64(n)
	if err != nil {
		return total, err
	}

	// Will
	if p.WillFlag {
		if p.ProtocolLevel >= 5 {
			n, err = w.Write(willPropsBytes)
			total += int64(n)
			if err != nil {
				return total, err
			}
		}
		n, err = w.Write(willTopicBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
		n, err = w.Write(willMsgBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// Username
	if p.UsernameFlag {
		n, err = w.Write(usernameBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	// Password
	if p.PasswordFlag {
		n, err = w.Write(passwordBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// DecodeConnect decodes a CONNECT packet from the buffer.
func DecodeConnect(buf []byte) (*ConnectPacket, error) {
	if len(buf) < 10 {
		return nil, fmt.Errorf("buffer too short for CONNECT packet")
	}

	pkt := &ConnectPacket{}

	offset := 0

	// Protocol name
	protocolName, n, err := decodeString(buf[offset:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode protocol name: %w", err)
	}
	pkt.ProtocolName = protocolName
	offset += n

	// Protocol level
	if offset >= len(buf) {
		return nil, fmt.Errorf("buffer too short for protocol level")
	}
	pkt.ProtocolLevel = buf[offset]
	offset++

	// Connect flags
	if offset >= len(buf) {
		return nil, fmt.Errorf("buffer too short for connect flags")
	}
	connectFlags := buf[offset]
	offset++

	pkt.CleanSession = (connectFlags & 0x02) != 0
	pkt.WillFlag = (connectFlags & 0x04) != 0
	pkt.WillQoS = (connectFlags >> 3) & 0x03
	pkt.WillRetain = (connectFlags & 0x20) != 0
	pkt.PasswordFlag = (connectFlags & 0x40) != 0
	pkt.UsernameFlag = (connectFlags & 0x80) != 0

	// Keep alive
	if offset+2 > len(buf) {
		return nil, fmt.Errorf("buffer too short for keep alive")
	}
	pkt.KeepAlive = uint16(buf[offset])<<8 | uint16(buf[offset+1])
	offset += 2

	// Properties (v5.0 only)
	if pkt.ProtocolLevel >= 5 {
		props, nProps, err := decodeProperties(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode properties: %w", err)
		}
		pkt.Properties = props
		offset += nProps
	}

	// Client ID
	clientID, n, err := decodeString(buf[offset:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode client ID: %w", err)
	}
	pkt.ClientID = clientID
	offset += n

	// Will topic and message
	if pkt.WillFlag {
		// Will Properties (v5.0 only)
		if pkt.ProtocolLevel >= 5 {
			props, nProps, err := decodeProperties(buf[offset:])
			if err != nil {
				return nil, fmt.Errorf("failed to decode will properties: %w", err)
			}
			pkt.WillProperties = props
			offset += nProps
		}

		willTopic, n, err := decodeString(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode will topic: %w", err)
		}
		pkt.WillTopic = willTopic
		offset += n

		willMessage, n, err := decodeBinary(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode will message: %w", err)
		}
		// Copy willMessage because the underlying buffer is reused
		pkt.WillMessage = make([]byte, len(willMessage))
		copy(pkt.WillMessage, willMessage)
		offset += n
	}

	// Username
	if pkt.UsernameFlag {
		username, n, err := decodeString(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode username: %w", err)
		}
		pkt.Username = username
		offset += n
	}

	// Password
	if pkt.PasswordFlag {
		password, _, err := decodeString(buf[offset:])
		if err != nil {
			return nil, fmt.Errorf("failed to decode password: %w", err)
		}
		pkt.Password = password
	}

	return pkt, nil
}
