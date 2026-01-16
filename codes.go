package mq

// MQTT v5.0 Reason Codes
//
// These constants represent the reason codes defined in the MQTT v5.0 specification.
// Reason codes are used in DISCONNECT, CONNACK, PUBACK, PUBREC, PUBREL, PUBCOMP,
// SUBACK, and UNSUBACK packets to provide detailed information about the outcome
// of an operation.
//
// Use these constants with the IsReasonCode function to check for specific error
// conditions returned by the server.
//
// Example (checking for specific disconnect reason):
//
//	token := client.Publish("topic", data, mq.WithQoS(1))
//	if err := token.Wait(ctx); err != nil {
//	    if mq.IsReasonCode(err, mq.ReasonCodeQuotaExceeded) {
//	        log.Println("Server quota exceeded, backing off...")
//	    } else if mq.IsReasonCode(err, mq.ReasonCodeNotAuthorized) {
//	        log.Println("Not authorized to publish to this topic")
//	    }
//	}
//
// Example (graceful disconnect with will):
//
//	client.Disconnect(ctx, mq.WithReason(mq.ReasonCodeDisconnectWithWill))
//
// Reason codes 0x00-0x7F indicate success, while 0x80-0xFF indicate failure.
const (
	ReasonCodeNormalDisconnect      uint8 = 0x00
	ReasonCodeDisconnectWithWill    uint8 = 0x04
	ReasonCodeUnspecifiedError      uint8 = 0x80
	ReasonCodeMalformedPacket       uint8 = 0x81
	ReasonCodeProtocolError         uint8 = 0x82
	ReasonCodeImplementationError   uint8 = 0x83
	ReasonCodeNotAuthorized         uint8 = 0x87
	ReasonCodeServerBusy            uint8 = 0x89
	ReasonCodeServerShuttingDown    uint8 = 0x8B
	ReasonCodeKeepAliveTimeout      uint8 = 0x8D
	ReasonCodeSessionTakenOver      uint8 = 0x8E
	ReasonCodeTopicFilterInvalid    uint8 = 0x90
	ReasonCodeTopicNameInvalid      uint8 = 0x91
	ReasonCodeReceiveMaximumExceed  uint8 = 0x93
	ReasonCodeTopicAliasInvalid     uint8 = 0x94
	ReasonCodePacketTooLarge        uint8 = 0x95
	ReasonCodeMessageRateTooHigh    uint8 = 0x96
	ReasonCodeQuotaExceeded         uint8 = 0x97
	ReasonCodeAdministrativeAction  uint8 = 0x98
	ReasonCodePayloadFormatInvalid  uint8 = 0x99
	ReasonCodeRetainNotSupported    uint8 = 0x9A
	ReasonCodeQoSNotSupported       uint8 = 0x9B
	ReasonCodeUseAnotherServer      uint8 = 0x9C
	ReasonCodeServerMoved           uint8 = 0x9D
	ReasonCodeSharedSubNotSupported uint8 = 0x9E
	ReasonCodeConnectionRateExceed  uint8 = 0x9F
	ReasonCodeMaximumConnectTime    uint8 = 0xA0
	ReasonCodeSubscriptionIDNotSupp uint8 = 0xA1
	ReasonCodeWildcardSubNotSupp    uint8 = 0xA2
)
