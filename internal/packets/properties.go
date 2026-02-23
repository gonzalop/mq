package packets

import (
	"encoding/binary"
	"fmt"
)

// Property IDs defined in MQTT v5.0 spec
const (
	PropPayloadFormatIndicator          uint8 = 0x01
	PropMessageExpiryInterval           uint8 = 0x02
	PropContentType                     uint8 = 0x03
	PropResponseTopic                   uint8 = 0x08
	PropCorrelationData                 uint8 = 0x09
	PropSubscriptionIdentifier          uint8 = 0x0B
	PropSessionExpiryInterval           uint8 = 0x11
	PropAssignedClientIdentifier        uint8 = 0x12
	PropServerKeepAlive                 uint8 = 0x13
	PropAuthenticationMethod            uint8 = 0x15
	PropAuthenticationData              uint8 = 0x16
	PropRequestProblemInformation       uint8 = 0x17
	PropWillDelayInterval               uint8 = 0x18
	PropRequestResponseInformation      uint8 = 0x19
	PropResponseInformation             uint8 = 0x1A
	PropServerReference                 uint8 = 0x1C
	PropReasonString                    uint8 = 0x1F
	PropReceiveMaximum                  uint8 = 0x21
	PropTopicAliasMaximum               uint8 = 0x22
	PropTopicAlias                      uint8 = 0x23
	PropMaximumQoS                      uint8 = 0x24
	PropRetainAvailable                 uint8 = 0x25
	PropUserProperty                    uint8 = 0x26
	PropMaximumPacketSize               uint8 = 0x27
	PropWildcardSubscriptionAvailable   uint8 = 0x28
	PropSubscriptionIdentifierAvailable uint8 = 0x29
	PropSharedSubscriptionAvailable     uint8 = 0x2A
)

// Presence flags for Properties struct
const (
	PresPayloadFormatIndicator          uint32 = 1 << 0
	PresMessageExpiryInterval           uint32 = 1 << 1
	PresContentType                     uint32 = 1 << 2
	PresResponseTopic                   uint32 = 1 << 3
	PresSessionExpiryInterval           uint32 = 1 << 4
	PresAssignedClientIdentifier        uint32 = 1 << 5
	PresServerKeepAlive                 uint32 = 1 << 6
	PresAuthenticationMethod            uint32 = 1 << 7
	PresRequestProblemInformation       uint32 = 1 << 8
	PresWillDelayInterval               uint32 = 1 << 9
	PresRequestResponseInformation      uint32 = 1 << 10
	PresResponseInformation             uint32 = 1 << 11
	PresServerReference                 uint32 = 1 << 12
	PresReasonString                    uint32 = 1 << 13
	PresReceiveMaximum                  uint32 = 1 << 14
	PresTopicAliasMaximum               uint32 = 1 << 15
	PresTopicAlias                      uint32 = 1 << 16
	PresMaximumQoS                      uint32 = 1 << 17
	PresRetainAvailable                 uint32 = 1 << 18
	PresMaximumPacketSize               uint32 = 1 << 19
	PresWildcardSubscriptionAvailable   uint32 = 1 << 20
	PresSubscriptionIdentifierAvailable uint32 = 1 << 21
	PresSharedSubscriptionAvailable     uint32 = 1 << 22
)

// Property represents a single MQTT property.
type Property struct {
	ID    uint8
	Value any
}

// UserProperty represents a key-value pair.
type UserProperty struct {
	Key   string
	Value string
}

// Properties holds all standard MQTT 5.0 properties.
// Optimized for allocation-free decoding using value types and a bitmask.
type Properties struct {
	Presence                        uint32
	PayloadFormatIndicator          uint8
	MessageExpiryInterval           uint32
	ContentType                     string
	ResponseTopic                   string
	CorrelationData                 []byte
	SubscriptionIdentifier          []int
	SessionExpiryInterval           uint32
	AssignedClientIdentifier        string
	ServerKeepAlive                 uint16
	AuthenticationMethod            string
	AuthenticationData              []byte
	RequestProblemInformation       uint8
	WillDelayInterval               uint32
	RequestResponseInformation      uint8
	ResponseInformation             string
	ServerReference                 string
	ReasonString                    string
	ReceiveMaximum                  uint16
	TopicAliasMaximum               uint16
	TopicAlias                      uint16
	MaximumQoS                      uint8
	RetainAvailable                 bool
	UserProperties                  []UserProperty
	MaximumPacketSize               uint32
	WildcardSubscriptionAvailable   bool
	SubscriptionIdentifierAvailable bool
	SharedSubscriptionAvailable     bool
}

// encodeProperties serializes the properties into the MQTT v5 format.
// Returns the bytes of the "Properties" section (Length + Props).
func encodeProperties(p *Properties) []byte {
	if p == nil {
		return []byte{0x00} // Length 0
	}
	// Pre-allocate a reasonable guess to avoid initial re-allocations
	return appendProperties(make([]byte, 0, 64), p)
}

// appendProperties appends the serialized properties to dst.
func appendProperties(dst []byte, p *Properties) []byte {
	if p == nil {
		return append(dst, 0x00)
	}

	startLen := len(dst)
	// optimistically assume 1 byte length (len < 128)
	dst = append(dst, 0)
	propsStart := len(dst)

	dst = p.appendNumeric(dst)
	dst = p.appendBool(dst)
	dst = p.appendStringOrBinary(dst)
	dst = p.appendSpecial(dst)

	// Calculate length of the properties data
	propLen := len(dst) - propsStart

	if propLen < 128 {
		dst[startLen] = byte(propLen)
		return dst
	}

	// If it doesn't fit, in 1 byte...
	lenBuf := encodeVarInt(propLen)
	lenDiff := len(lenBuf) - 1 // we already have 1 byte reserved

	dst = append(dst, make([]byte, lenDiff)...)
	copy(dst[propsStart+lenDiff:], dst[propsStart:propsStart+propLen])
	copy(dst[startLen:], lenBuf)

	return dst
}

// decodeProperties reads the properties from the buffer.
// Returns the properties and the number of bytes read (including length).
func decodeProperties(buf []byte) (*Properties, int, error) {
	if len(buf) == 0 {
		return nil, 0, fmt.Errorf("buffer too short for properties length")
	}

	propLen, n, err := decodeVarIntBuf(buf)
	if err != nil {
		return nil, 0, err
	}
	totalLen := n + propLen

	if len(buf) < totalLen {
		return nil, 0, fmt.Errorf("buffer too short for properties data")
	}

	if propLen == 0 {
		return nil, totalLen, nil
	}

	p := &Properties{}
	slice := buf[n:totalLen] // View into the properties data
	offset := 0

	for offset < len(slice) {
		id := slice[offset]
		offset++

		// Try numeric
		nProp, ok, err := p.decodeNumeric(id, slice[offset:])
		if err != nil {
			return nil, 0, err
		}
		if ok {
			offset += nProp
			continue
		}

		// Try bool
		nProp, ok, err = p.decodeBool(id, slice[offset:])
		if err != nil {
			return nil, 0, err
		}
		if ok {
			offset += nProp
			continue
		}

		// Try string/binary
		nProp, ok, err = p.decodeStringOrBinary(id, slice[offset:])
		if err != nil {
			return nil, 0, err
		}
		if ok {
			offset += nProp
			continue
		}

		// Try special
		nProp, ok, err = p.decodeSpecial(id, slice[offset:])
		if err != nil {
			return nil, 0, err
		}
		if ok {
			offset += nProp
			continue
		}

		// Unknown
		return nil, 0, fmt.Errorf("unsupported property ID: 0x%02x", id)
	}

	return p, totalLen, nil
}

func (p *Properties) appendNumeric(dst []byte) []byte {
	if p.Presence&PresPayloadFormatIndicator != 0 {
		dst = append(dst, PropPayloadFormatIndicator, p.PayloadFormatIndicator)
	}
	if p.Presence&PresMessageExpiryInterval != 0 {
		dst = append(dst, PropMessageExpiryInterval)
		dst = binary.BigEndian.AppendUint32(dst, p.MessageExpiryInterval)
	}
	if p.Presence&PresSessionExpiryInterval != 0 {
		dst = append(dst, PropSessionExpiryInterval)
		dst = binary.BigEndian.AppendUint32(dst, p.SessionExpiryInterval)
	}
	if p.Presence&PresServerKeepAlive != 0 {
		dst = append(dst, PropServerKeepAlive)
		dst = binary.BigEndian.AppendUint16(dst, p.ServerKeepAlive)
	}
	if p.Presence&PresRequestProblemInformation != 0 {
		dst = append(dst, PropRequestProblemInformation, p.RequestProblemInformation)
	}
	if p.Presence&PresWillDelayInterval != 0 {
		dst = append(dst, PropWillDelayInterval)
		dst = binary.BigEndian.AppendUint32(dst, p.WillDelayInterval)
	}
	if p.Presence&PresRequestResponseInformation != 0 {
		dst = append(dst, PropRequestResponseInformation, p.RequestResponseInformation)
	}
	if p.Presence&PresReceiveMaximum != 0 {
		dst = append(dst, PropReceiveMaximum)
		dst = binary.BigEndian.AppendUint16(dst, p.ReceiveMaximum)
	}
	if p.Presence&PresTopicAliasMaximum != 0 {
		dst = append(dst, PropTopicAliasMaximum)
		dst = binary.BigEndian.AppendUint16(dst, p.TopicAliasMaximum)
	}
	if p.Presence&PresTopicAlias != 0 {
		dst = append(dst, PropTopicAlias)
		dst = binary.BigEndian.AppendUint16(dst, p.TopicAlias)
	}
	if p.Presence&PresMaximumQoS != 0 {
		dst = append(dst, PropMaximumQoS, p.MaximumQoS)
	}
	if p.Presence&PresMaximumPacketSize != 0 {
		dst = append(dst, PropMaximumPacketSize)
		dst = binary.BigEndian.AppendUint32(dst, p.MaximumPacketSize)
	}
	return dst
}

func (p *Properties) appendBool(dst []byte) []byte {
	if p.Presence&PresRetainAvailable != 0 {
		val := byte(0)
		if p.RetainAvailable {
			val = 1
		}
		dst = append(dst, PropRetainAvailable, val)
	}
	if p.Presence&PresWildcardSubscriptionAvailable != 0 {
		val := byte(0)
		if p.WildcardSubscriptionAvailable {
			val = 1
		}
		dst = append(dst, PropWildcardSubscriptionAvailable, val)
	}
	if p.Presence&PresSubscriptionIdentifierAvailable != 0 {
		val := byte(0)
		if p.SubscriptionIdentifierAvailable {
			val = 1
		}
		dst = append(dst, PropSubscriptionIdentifierAvailable, val)
	}
	if p.Presence&PresSharedSubscriptionAvailable != 0 {
		val := byte(0)
		if p.SharedSubscriptionAvailable {
			val = 1
		}
		dst = append(dst, PropSharedSubscriptionAvailable, val)
	}
	return dst
}

func (p *Properties) appendStringOrBinary(dst []byte) []byte {
	if p.Presence&PresContentType != 0 {
		dst = append(dst, PropContentType)
		dst = appendString(dst, p.ContentType)
	}
	if p.Presence&PresResponseTopic != 0 {
		dst = append(dst, PropResponseTopic)
		dst = appendString(dst, p.ResponseTopic)
	}
	if len(p.CorrelationData) > 0 {
		dst = append(dst, PropCorrelationData)
		dst = appendBinary(dst, p.CorrelationData)
	}
	if p.Presence&PresAssignedClientIdentifier != 0 {
		dst = append(dst, PropAssignedClientIdentifier)
		dst = appendString(dst, p.AssignedClientIdentifier)
	}
	if p.Presence&PresAuthenticationMethod != 0 {
		dst = append(dst, PropAuthenticationMethod)
		dst = appendString(dst, p.AuthenticationMethod)
	}
	if len(p.AuthenticationData) > 0 {
		dst = append(dst, PropAuthenticationData)
		dst = appendBinary(dst, p.AuthenticationData)
	}
	if p.Presence&PresResponseInformation != 0 {
		dst = append(dst, PropResponseInformation)
		dst = appendString(dst, p.ResponseInformation)
	}
	if p.Presence&PresServerReference != 0 {
		dst = append(dst, PropServerReference)
		dst = appendString(dst, p.ServerReference)
	}
	if p.Presence&PresReasonString != 0 {
		dst = append(dst, PropReasonString)
		dst = appendString(dst, p.ReasonString)
	}
	return dst
}

func (p *Properties) appendSpecial(dst []byte) []byte {
	if len(p.SubscriptionIdentifier) > 0 {
		for _, id := range p.SubscriptionIdentifier {
			dst = append(dst, PropSubscriptionIdentifier)
			dst = appendVarInt(dst, id)
		}
	}
	if len(p.UserProperties) > 0 {
		for _, up := range p.UserProperties {
			dst = append(dst, PropUserProperty)
			dst = appendString(dst, up.Key)
			dst = appendString(dst, up.Value)
		}
	}
	return dst
}

func (p *Properties) decodeNumeric(id byte, data []byte) (int, bool, error) {
	switch id {
	case PropPayloadFormatIndicator:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.PayloadFormatIndicator = data[0]
		p.Presence |= PresPayloadFormatIndicator
		return 1, true, nil
	case PropMessageExpiryInterval:
		if len(data) < 4 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.MessageExpiryInterval = binary.BigEndian.Uint32(data)
		p.Presence |= PresMessageExpiryInterval
		return 4, true, nil
	case PropSessionExpiryInterval:
		if len(data) < 4 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.SessionExpiryInterval = binary.BigEndian.Uint32(data)
		p.Presence |= PresSessionExpiryInterval
		return 4, true, nil
	case PropServerKeepAlive:
		if len(data) < 2 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.ServerKeepAlive = binary.BigEndian.Uint16(data)
		p.Presence |= PresServerKeepAlive
		return 2, true, nil
	case PropRequestProblemInformation:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.RequestProblemInformation = data[0]
		p.Presence |= PresRequestProblemInformation
		return 1, true, nil
	case PropWillDelayInterval:
		if len(data) < 4 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.WillDelayInterval = binary.BigEndian.Uint32(data)
		p.Presence |= PresWillDelayInterval
		return 4, true, nil
	case PropRequestResponseInformation:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.RequestResponseInformation = data[0]
		p.Presence |= PresRequestResponseInformation
		return 1, true, nil
	case PropReceiveMaximum:
		if len(data) < 2 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.ReceiveMaximum = binary.BigEndian.Uint16(data)
		p.Presence |= PresReceiveMaximum
		return 2, true, nil
	case PropTopicAliasMaximum:
		if len(data) < 2 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.TopicAliasMaximum = binary.BigEndian.Uint16(data)
		p.Presence |= PresTopicAliasMaximum
		return 2, true, nil
	case PropTopicAlias:
		if len(data) < 2 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.TopicAlias = binary.BigEndian.Uint16(data)
		p.Presence |= PresTopicAlias
		return 2, true, nil
	case PropMaximumQoS:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.MaximumQoS = data[0]
		p.Presence |= PresMaximumQoS
		return 1, true, nil
	case PropMaximumPacketSize:
		if len(data) < 4 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.MaximumPacketSize = binary.BigEndian.Uint32(data)
		p.Presence |= PresMaximumPacketSize
		return 4, true, nil
	}
	return 0, false, nil
}

func (p *Properties) decodeBool(id byte, data []byte) (int, bool, error) {
	switch id {
	case PropRetainAvailable:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.RetainAvailable = data[0] != 0
		p.Presence |= PresRetainAvailable
		return 1, true, nil
	case PropWildcardSubscriptionAvailable:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.WildcardSubscriptionAvailable = data[0] != 0
		p.Presence |= PresWildcardSubscriptionAvailable
		return 1, true, nil
	case PropSubscriptionIdentifierAvailable:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.SubscriptionIdentifierAvailable = data[0] != 0
		p.Presence |= PresSubscriptionIdentifierAvailable
		return 1, true, nil
	case PropSharedSubscriptionAvailable:
		if len(data) < 1 {
			return 0, false, fmt.Errorf("malformed property 0x%02x", id)
		}
		p.SharedSubscriptionAvailable = data[0] != 0
		p.Presence |= PresSharedSubscriptionAvailable
		return 1, true, nil
	}
	return 0, false, nil
}

func (p *Properties) decodeStringOrBinary(id byte, data []byte) (int, bool, error) {
	switch id {
	case PropContentType:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.ContentType = s
		p.Presence |= PresContentType
		return n, true, nil
	case PropResponseTopic:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.ResponseTopic = s
		p.Presence |= PresResponseTopic
		return n, true, nil
	case PropCorrelationData:
		b, n, err := decodeBinary(data)
		if err != nil {
			return 0, false, err
		}
		p.CorrelationData = b
		return n, true, nil
	case PropAssignedClientIdentifier:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.AssignedClientIdentifier = s
		p.Presence |= PresAssignedClientIdentifier
		return n, true, nil
	case PropAuthenticationMethod:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.AuthenticationMethod = s
		p.Presence |= PresAuthenticationMethod
		return n, true, nil
	case PropAuthenticationData:
		b, n, err := decodeBinary(data)
		if err != nil {
			return 0, false, err
		}
		p.AuthenticationData = b
		return n, true, nil
	case PropResponseInformation:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.ResponseInformation = s
		p.Presence |= PresResponseInformation
		return n, true, nil
	case PropServerReference:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.ServerReference = s
		p.Presence |= PresServerReference
		return n, true, nil
	case PropReasonString:
		s, n, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		p.ReasonString = s
		p.Presence |= PresReasonString
		return n, true, nil
	}
	return 0, false, nil
}

func (p *Properties) decodeSpecial(id byte, data []byte) (int, bool, error) {
	switch id {
	case PropUserProperty:
		k, nK, err := decodeString(data)
		if err != nil {
			return 0, false, err
		}
		v, nV, err := decodeString(data[nK:])
		if err != nil {
			return 0, false, err
		}
		p.UserProperties = append(p.UserProperties, UserProperty{Key: k, Value: v})
		return nK + nV, true, nil
	case PropSubscriptionIdentifier:
		val, n, err := decodeVarIntBuf(data)
		if err != nil {
			return 0, false, err
		}
		p.SubscriptionIdentifier = append(p.SubscriptionIdentifier, val)
		return n, true, nil
	}
	return 0, false, nil
}
