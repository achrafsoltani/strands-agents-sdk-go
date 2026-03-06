package bedrock

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// AWS event stream uses CRC32 IEEE, NOT CRC32C (Castagnoli).
// Despite some AWS docs referencing "CRC32", the actual wire format and the
// aws-sdk-go-v2 implementation (aws/protocol/eventstream) use crc32.IEEE.
var crc32IEEETable = crc32.IEEETable

// eventStreamMessage represents a decoded AWS event stream message.
type eventStreamMessage struct {
	Headers map[string]string
	Payload []byte
}

// decodeEventStream reads one binary-framed message from an AWS event stream.
// Returns io.EOF when the stream ends cleanly.
func decodeEventStream(r io.Reader) (*eventStreamMessage, error) {
	// 12-byte prelude: total_length(4) + headers_length(4) + prelude_crc(4).
	var prelude [12]byte
	if _, err := io.ReadFull(r, prelude[:]); err != nil {
		return nil, err
	}

	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	preludeCRC := binary.BigEndian.Uint32(prelude[8:12])

	if eventCRC(prelude[0:8]) != preludeCRC {
		return nil, fmt.Errorf("bedrock: event stream prelude CRC mismatch")
	}

	if totalLength < 16 || headersLength > totalLength-16 {
		return nil, fmt.Errorf("bedrock: invalid event stream frame lengths")
	}

	payloadLength := totalLength - 12 - headersLength - 4

	// Read headers + payload.
	body := make([]byte, headersLength+payloadLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("bedrock: failed to read event body: %w", err)
	}

	// Read and verify message CRC.
	var crcBuf [4]byte
	if _, err := io.ReadFull(r, crcBuf[:]); err != nil {
		return nil, fmt.Errorf("bedrock: failed to read message CRC: %w", err)
	}
	messageCRC := binary.BigEndian.Uint32(crcBuf[:])

	// Message CRC covers prelude + headers + payload.
	var fullMsg []byte
	fullMsg = append(fullMsg, prelude[:]...)
	fullMsg = append(fullMsg, body...)
	if eventCRC(fullMsg) != messageCRC {
		return nil, fmt.Errorf("bedrock: event stream message CRC mismatch")
	}

	headers := parseEventHeaders(body[:headersLength])

	return &eventStreamMessage{
		Headers: headers,
		Payload: body[headersLength:],
	}, nil
}

// parseEventHeaders decodes the binary header section of an event stream message.
func parseEventHeaders(data []byte) map[string]string {
	headers := make(map[string]string)
	pos := 0
	for pos < len(data) {
		nameLen := int(data[pos])
		pos++
		if pos+nameLen > len(data) {
			break
		}
		name := string(data[pos : pos+nameLen])
		pos += nameLen
		if pos >= len(data) {
			break
		}
		valueType := data[pos]
		pos++
		switch valueType {
		case 7: // String
			if pos+2 > len(data) {
				return headers
			}
			strLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2
			if pos+strLen > len(data) {
				return headers
			}
			headers[name] = string(data[pos : pos+strLen])
			pos += strLen
		case 0: // Bool true
			headers[name] = "true"
		case 1: // Bool false
			headers[name] = "false"
		case 2: // Byte
			pos++
		case 3: // Short
			pos += 2
		case 4: // Int
			pos += 4
		case 5: // Long
			pos += 8
		case 6: // Bytes
			if pos+2 > len(data) {
				return headers
			}
			bLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2 + bLen
		case 8: // Timestamp
			pos += 8
		case 9: // UUID
			pos += 16
		default:
			return headers
		}
	}
	return headers
}

func eventCRC(data []byte) uint32 {
	return crc32.Checksum(data, crc32IEEETable)
}
