package mq

import "fmt"

// ReasonCode represents a MQTT v5.0 reason code.
// It implements the error interface so it can be used with errors.Is.
type ReasonCode uint8

func (r ReasonCode) Error() string {
	return fmt.Sprintf("mqtt reason code 0x%02X", uint8(r))
}

// MQTT v5.0 Reason Codes
//
// These constants represent the reason codes defined in the MQTT v5.0 specification.
// Reason codes are used in DISCONNECT, CONNACK, PUBACK, PUBREC, PUBREL, PUBCOMP,
// SUBACK, and UNSUBACK packets to provide detailed information about the outcome
// of an operation.
//
// Use these constants with errors.Is to check for specific error conditions.
//
// Example (checking for specific disconnect reason):
//
//	token := client.Publish("topic", data, mq.WithQoS(1))
//	if err := token.Wait(ctx); err != nil {
//	    if errors.Is(err, mq.ReasonCodeQuotaExceeded) {
//	        log.Println("Server quota exceeded, backing off...")
//	    } else if errors.Is(err, mq.ReasonCodeNotAuthorized) {
//	        log.Println("Not authorized to publish to this topic")
//	    }
//	}
//
// Reason codes 0x00-0x7F indicate success, while 0x80-0xFF indicate failure.
const (
	ReasonCodeNormalDisconnect      ReasonCode = 0x00
	ReasonCodeDisconnectWithWill    ReasonCode = 0x04
	ReasonCodeUnspecifiedError      ReasonCode = 0x80
	ReasonCodeMalformedPacket       ReasonCode = 0x81
	ReasonCodeProtocolError         ReasonCode = 0x82
	ReasonCodeImplementationError   ReasonCode = 0x83
	ReasonCodeNotAuthorized         ReasonCode = 0x87
	ReasonCodeServerBusy            ReasonCode = 0x89
	ReasonCodeServerShuttingDown    ReasonCode = 0x8B
	ReasonCodeKeepAliveTimeout      ReasonCode = 0x8D
	ReasonCodeSessionTakenOver      ReasonCode = 0x8E
	ReasonCodeTopicFilterInvalid    ReasonCode = 0x90
	ReasonCodeTopicNameInvalid      ReasonCode = 0x91
	ReasonCodeReceiveMaximumExceed  ReasonCode = 0x93
	ReasonCodeTopicAliasInvalid     ReasonCode = 0x94
	ReasonCodePacketTooLarge        ReasonCode = 0x95
	ReasonCodeMessageRateTooHigh    ReasonCode = 0x96
	ReasonCodeQuotaExceeded         ReasonCode = 0x97
	ReasonCodeAdministrativeAction  ReasonCode = 0x98
	ReasonCodePayloadFormatInvalid  ReasonCode = 0x99
	ReasonCodeRetainNotSupported    ReasonCode = 0x9A
	ReasonCodeQoSNotSupported       ReasonCode = 0x9B
	ReasonCodeUseAnotherServer      ReasonCode = 0x9C
	ReasonCodeServerMoved           ReasonCode = 0x9D
	ReasonCodeSharedSubNotSupported ReasonCode = 0x9E
	ReasonCodeConnectionRateExceed  ReasonCode = 0x9F
	ReasonCodeMaximumConnectTime    ReasonCode = 0xA0
	ReasonCodeSubscriptionIDNotSupp ReasonCode = 0xA1
	ReasonCodeWildcardSubNotSupp    ReasonCode = 0xA2
)
