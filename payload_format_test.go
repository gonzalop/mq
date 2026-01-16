package mq

import (
	"testing"
)

func TestValidatePayloadFormat(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		props   *Properties
		wantErr bool
	}{
		{
			name:    "No Properties",
			payload: []byte{0xFF, 0xFE}, // Invalid UTF-8
			props:   nil,
			wantErr: false,
		},
		{
			name:    "No Payload Format",
			payload: []byte{0xFF, 0xFE},
			props:   &Properties{},
			wantErr: false,
		},
		{
			name:    "Payload Format Bytes (0)",
			payload: []byte{0xFF, 0xFE},
			props: &Properties{
				PayloadFormat: ptr(PayloadFormatBytes),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Valid",
			payload: []byte("Hello World"),
			props: &Properties{
				PayloadFormat: ptr(PayloadFormatUTF8),
			},
			wantErr: false,
		},
		{
			name:    "Payload Format UTF-8 (1) - Invalid",
			payload: []byte{0xFF, 0xFE},
			props: &Properties{
				PayloadFormat: ptr(PayloadFormatUTF8),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePayloadFormat(tt.payload, tt.props)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePayloadFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func ptr(v uint8) *uint8 {
	return &v
}
