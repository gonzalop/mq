package packets

import (
	"encoding/binary"
	"fmt"
	"io"
)

func writeAckPacket(w io.Writer, pktType, flags uint8, packetID uint16, version, reasonCode uint8, props *Properties) (int64, error) {
	var total int64
	var propsBytes []byte
	variableHeaderLen := 2

	hasV5Fields := version >= 5 && (reasonCode != 0 || props != nil)
	if hasV5Fields {
		propsBytes = encodeProperties(props)
		variableHeaderLen += 1 + len(propsBytes)
	}

	header := &FixedHeader{
		PacketType:      pktType,
		Flags:           flags,
		RemainingLength: variableHeaderLen,
	}
	hN, err := header.WriteTo(w)
	total += hN
	if err != nil {
		return total, err
	}

	var packetIDBytes [2]byte
	binary.BigEndian.PutUint16(packetIDBytes[:], packetID)
	n, err := w.Write(packetIDBytes[:])
	total += int64(n)
	if err != nil {
		return total, err
	}

	if hasV5Fields {
		if err := writeByte(w, reasonCode); err != nil {
			return total, err
		}
		total++

		n, err = w.Write(propsBytes)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}

	return total, nil
}

func decodeAckPacket(buf []byte, version uint8, name string) (packetID uint16, reasonCode uint8, props *Properties, err error) {
	if len(buf) < 2 {
		return 0, 0, nil, fmt.Errorf("buffer too short for %s packet", name)
	}

	packetID = binary.BigEndian.Uint16(buf[0:2])

	if version >= 5 && len(buf) > 2 {
		reasonCode = buf[2]
		if len(buf) > 3 {
			props, _, err = decodeProperties(buf[3:])
			if err != nil {
				return 0, 0, nil, fmt.Errorf("failed to decode properties: %w", err)
			}
		}
	}

	return packetID, reasonCode, props, nil
}
