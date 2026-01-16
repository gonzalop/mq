package packets

// MQTT Control Packet types
const (
	RESERVED    = 0
	CONNECT     = 1
	CONNACK     = 2
	PUBLISH     = 3
	PUBACK      = 4
	PUBREC      = 5
	PUBREL      = 6
	PUBCOMP     = 7
	SUBSCRIBE   = 8
	SUBACK      = 9
	UNSUBSCRIBE = 10
	UNSUBACK    = 11
	PINGREQ     = 12
	PINGRESP    = 13
	DISCONNECT  = 14
	AUTH        = 15 // MQTT v5.0 only
)

// PacketNames maps property IDs to human-readable names
var PacketNames = map[uint8]string{
	RESERVED:    "RESERVED",
	CONNECT:     "CONNECT",
	CONNACK:     "CONNACK",
	PUBLISH:     "PUBLISH",
	PUBACK:      "PUBACK",
	PUBREC:      "PUBREC",
	PUBREL:      "PUBREL",
	PUBCOMP:     "PUBCOMP",
	SUBSCRIBE:   "SUBSCRIBE",
	SUBACK:      "SUBACK",
	UNSUBSCRIBE: "UNSUBSCRIBE",
	UNSUBACK:    "UNSUBACK",
	PINGREQ:     "PINGREQ",
	PINGRESP:    "PINGRESP",
	DISCONNECT:  "DISCONNECT",
	AUTH:        "AUTH",
}

// QoS levels
const (
	QoS0 = 0 // At most once
	QoS1 = 1 // At least once
	QoS2 = 2 // Exactly once
)

// CONNACK return codes (v3.1.1)
const (
	ConnAccepted                     = 0
	ConnRefusedUnacceptableProtocol  = 1
	ConnRefusedIdentifierRejected    = 2
	ConnRefusedServerUnavailable     = 3
	ConnRefusedBadUsernameOrPassword = 4
	ConnRefusedNotAuthorized         = 5
)

// SUBACK return codes
const (
	SubackQoS0    = 0x00
	SubackQoS1    = 0x01
	SubackQoS2    = 0x02
	SubackFailure = 0x80
)
