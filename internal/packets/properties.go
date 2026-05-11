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
	PresCorrelationData                 uint32 = 1 << 23
	PresAuthenticationData              uint32 = 1 << 24
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

type propType uint8

const (
	typeByte propType = iota
	typeTwoByte
	typeFourByte
	typeString
	typeBinary
	typeVarInt
	typeUser
)

type propInfo struct {
	presence uint32
	kind     propType
}

var propertyTable = [256]propInfo{
	PropPayloadFormatIndicator:          {PresPayloadFormatIndicator, typeByte},
	PropMessageExpiryInterval:           {PresMessageExpiryInterval, typeFourByte},
	PropContentType:                     {PresContentType, typeString},
	PropResponseTopic:                   {PresResponseTopic, typeString},
	PropCorrelationData:                 {PresCorrelationData, typeBinary},
	PropSubscriptionIdentifier:          {0, typeVarInt},
	PropSessionExpiryInterval:           {PresSessionExpiryInterval, typeFourByte},
	PropAssignedClientIdentifier:        {PresAssignedClientIdentifier, typeString},
	PropServerKeepAlive:                 {PresServerKeepAlive, typeTwoByte},
	PropAuthenticationMethod:            {PresAuthenticationMethod, typeString},
	PropAuthenticationData:              {PresAuthenticationData, typeBinary},
	PropRequestProblemInformation:       {PresRequestProblemInformation, typeByte},
	PropWillDelayInterval:               {PresWillDelayInterval, typeFourByte},
	PropRequestResponseInformation:      {PresRequestResponseInformation, typeByte},
	PropResponseInformation:             {PresResponseInformation, typeString},
	PropServerReference:                 {PresServerReference, typeString},
	PropReasonString:                    {PresReasonString, typeString},
	PropReceiveMaximum:                  {PresReceiveMaximum, typeTwoByte},
	PropTopicAliasMaximum:               {PresTopicAliasMaximum, typeTwoByte},
	PropTopicAlias:                      {PresTopicAlias, typeTwoByte},
	PropMaximumQoS:                      {PresMaximumQoS, typeByte},
	PropRetainAvailable:                 {PresRetainAvailable, typeByte},
	PropUserProperty:                    {0, typeUser},
	PropMaximumPacketSize:               {PresMaximumPacketSize, typeFourByte},
	PropWildcardSubscriptionAvailable:   {PresWildcardSubscriptionAvailable, typeByte},
	PropSubscriptionIdentifierAvailable: {PresSubscriptionIdentifierAvailable, typeByte},
	PropSharedSubscriptionAvailable:     {PresSharedSubscriptionAvailable, typeByte},
}

// encodeProperties serializes the properties into the MQTT v5 format.
func encodeProperties(p *Properties) []byte {
	if p == nil {
		return []byte{0x00}
	}
	return appendProperties(make([]byte, 0, 64), p)
}

// appendProperties appends the serialized properties to dst.
func appendProperties(dst []byte, p *Properties) []byte {
	if p == nil {
		return append(dst, 0x00)
	}

	startLen := len(dst)
	dst = append(dst, 0)
	propsStart := len(dst)

	dst = p.appendNumeric(dst)
	dst = p.appendBool(dst)
	dst = p.appendStringOrBinary(dst)
	dst = p.appendSpecial(dst)

	propLen := len(dst) - propsStart
	if propLen < 128 {
		dst[startLen] = byte(propLen)
		return dst
	}

	lenBuf := encodeVarInt(propLen)
	lenDiff := len(lenBuf) - 1
	dst = append(dst, make([]byte, lenDiff)...)
	copy(dst[propsStart+lenDiff:], dst[propsStart:propsStart+propLen])
	copy(dst[startLen:], lenBuf)
	return dst
}

// decodeProperties reads the properties from the buffer.
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
	slice := buf[n:totalLen]
	offset := 0

	for offset < len(slice) {
		id := slice[offset]
		offset++

		info := propertyTable[id]
		if info.kind == 0 && id != PropPayloadFormatIndicator {
			if id > 0x2A || (id > 0x03 && id < 0x08) || id == 0x0A || (id > 0x0B && id < 0x11) {
				return nil, 0, fmt.Errorf("unsupported property ID: 0x%02x", id)
			}
		}

		if info.presence != 0 && p.Presence&info.presence != 0 {
			return nil, 0, fmt.Errorf("protocol error: duplicate property 0x%02x", id)
		}

		data := slice[offset:]
		var readN int

		switch info.kind {
		case typeByte:
			if len(data) < 1 {
				return nil, 0, fmt.Errorf("malformed property 0x%02x", id)
			}
			val := data[0]
			readN = 1
			switch id {
			case PropPayloadFormatIndicator:
				p.PayloadFormatIndicator = val
			case PropRequestProblemInformation:
				p.RequestProblemInformation = val
			case PropRequestResponseInformation:
				p.RequestResponseInformation = val
			case PropMaximumQoS:
				p.MaximumQoS = val
			case PropRetainAvailable:
				p.RetainAvailable = val != 0
			case PropWildcardSubscriptionAvailable:
				p.WildcardSubscriptionAvailable = val != 0
			case PropSubscriptionIdentifierAvailable:
				p.SubscriptionIdentifierAvailable = val != 0
			case PropSharedSubscriptionAvailable:
				p.SharedSubscriptionAvailable = val != 0
			}
		case typeTwoByte:
			if len(data) < 2 {
				return nil, 0, fmt.Errorf("malformed property 0x%02x", id)
			}
			val := binary.BigEndian.Uint16(data)
			readN = 2
			switch id {
			case PropServerKeepAlive:
				p.ServerKeepAlive = val
			case PropReceiveMaximum:
				p.ReceiveMaximum = val
			case PropTopicAliasMaximum:
				p.TopicAliasMaximum = val
			case PropTopicAlias:
				p.TopicAlias = val
			}
		case typeFourByte:
			if len(data) < 4 {
				return nil, 0, fmt.Errorf("malformed property 0x%02x", id)
			}
			val := binary.BigEndian.Uint32(data)
			readN = 4
			switch id {
			case PropMessageExpiryInterval:
				p.MessageExpiryInterval = val
			case PropSessionExpiryInterval:
				p.SessionExpiryInterval = val
			case PropWillDelayInterval:
				p.WillDelayInterval = val
			case PropMaximumPacketSize:
				p.MaximumPacketSize = val
			}
		case typeString:
			s, n, err := decodeString(data)
			if err != nil {
				return nil, 0, err
			}
			readN = n
			switch id {
			case PropContentType:
				p.ContentType = s
			case PropResponseTopic:
				p.ResponseTopic = s
			case PropAssignedClientIdentifier:
				p.AssignedClientIdentifier = s
			case PropAuthenticationMethod:
				p.AuthenticationMethod = s
			case PropResponseInformation:
				p.ResponseInformation = s
			case PropServerReference:
				p.ServerReference = s
			case PropReasonString:
				p.ReasonString = s
			}
		case typeBinary:
			b, n, err := decodeBinary(data)
			if err != nil {
				return nil, 0, err
			}
			readN = n
			switch id {
			case PropCorrelationData:
				p.CorrelationData = b
			case PropAuthenticationData:
				p.AuthenticationData = b
			}
		case typeVarInt:
			val, n, err := decodeVarIntBuf(data)
			if err != nil {
				return nil, 0, err
			}
			readN = n
			p.SubscriptionIdentifier = append(p.SubscriptionIdentifier, val)
		case typeUser:
			k, nK, err := decodeString(data)
			if err != nil {
				return nil, 0, err
			}
			v, nV, err := decodeString(data[nK:])
			if err != nil {
				return nil, 0, err
			}
			readN = nK + nV
			p.UserProperties = append(p.UserProperties, UserProperty{Key: k, Value: v})
		default:
			return nil, 0, fmt.Errorf("unsupported property ID: 0x%02x", id)
		}

		if info.presence != 0 {
			p.Presence |= info.presence
		}
		offset += readN
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
	for _, id := range p.SubscriptionIdentifier {
		dst = append(dst, PropSubscriptionIdentifier)
		dst = appendVarInt(dst, id)
	}
	for _, up := range p.UserProperties {
		dst = append(dst, PropUserProperty)
		dst = appendString(dst, up.Key)
		dst = appendString(dst, up.Value)
	}
	return dst
}
