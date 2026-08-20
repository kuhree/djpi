package wireless

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	frameSOF       = 0x55
	frameVersion   = 1
	frameHeaderLen = 11
	frameMinimum   = frameHeaderLen + 2
	frameMaximum   = 0x3ff
)

// SequenceOrder identifies the one transport-dependent field in a DUML frame.
// DJI BLE uses a big-endian message ID; the UDP datalink uses a little-endian
// sequence number.
type SequenceOrder uint8

const (
	SequenceBLE SequenceOrder = iota
	SequenceUDP
)

// Frame is the transport-independent part of one CRC-validated DUML message.
// It intentionally does not perform I/O or assign meanings to commands.
type Frame struct {
	Sender     byte
	Receiver   byte
	Sequence   uint16
	Flags      byte
	CommandSet byte
	CommandID  byte
	Payload    []byte
}

// ParseFrame validates and decodes exactly one DUML frame. Trailing bytes are
// rejected so callers cannot accidentally accept a truncated or concatenated
// transport message as a single command.
func ParseFrame(data []byte, order SequenceOrder) (Frame, error) {
	if err := validSequenceOrder(order); err != nil {
		return Frame{}, err
	}
	if len(data) < frameMinimum {
		return Frame{}, fmt.Errorf("DUML frame is %d bytes; minimum is %d", len(data), frameMinimum)
	}
	if data[0] != frameSOF {
		return Frame{}, fmt.Errorf("DUML frame has invalid start byte %#02x", data[0])
	}
	length := int(data[1]) | int(data[2]&0x03)<<8
	version := data[2] >> 2
	if version != frameVersion {
		return Frame{}, fmt.Errorf("unsupported DUML frame version %d", version)
	}
	if length < frameMinimum || length > frameMaximum {
		return Frame{}, fmt.Errorf("invalid DUML frame length %d", length)
	}
	if length != len(data) {
		return Frame{}, fmt.Errorf("DUML frame declares %d bytes but received %d", length, len(data))
	}
	if actual := crc8(data[:3]); actual != data[3] {
		return Frame{}, fmt.Errorf("DUML header CRC mismatch: got %#02x, want %#02x", data[3], actual)
	}
	wantCRC := binary.LittleEndian.Uint16(data[length-2:])
	if actual := crc16(data[:length-2]); actual != wantCRC {
		return Frame{}, fmt.Errorf("DUML frame CRC mismatch: got %#04x, want %#04x", wantCRC, actual)
	}
	return Frame{
		Sender:     data[4],
		Receiver:   data[5],
		Sequence:   decodeSequence(data[6:8], order),
		Flags:      data[8],
		CommandSet: data[9],
		CommandID:  data[10],
		Payload:    append([]byte(nil), data[11:length-2]...),
	}, nil
}

// Encode serializes one DUML frame and computes both protocol CRCs.
func (frame Frame) Encode(order SequenceOrder) ([]byte, error) {
	if err := validSequenceOrder(order); err != nil {
		return nil, err
	}
	length := frameMinimum + len(frame.Payload)
	if length > frameMaximum {
		return nil, fmt.Errorf("DUML frame length %d exceeds %d-byte protocol maximum", length, frameMaximum)
	}
	encoded := make([]byte, length)
	encoded[0] = frameSOF
	encoded[1] = byte(length)
	encoded[2] = byte(frameVersion<<2) | byte(length>>8)
	encoded[3] = crc8(encoded[:3])
	encoded[4] = frame.Sender
	encoded[5] = frame.Receiver
	encodeSequence(encoded[6:8], frame.Sequence, order)
	encoded[8] = frame.Flags
	encoded[9] = frame.CommandSet
	encoded[10] = frame.CommandID
	copy(encoded[11:], frame.Payload)
	binary.LittleEndian.PutUint16(encoded[length-2:], crc16(encoded[:length-2]))
	return encoded, nil
}

// PackString encodes the length-prefixed UTF-8 strings used by DJI wireless
// payloads. It only constructs bytes; it does not send or authorize pairing.
func PackString(value string) ([]byte, error) {
	if !utf8.ValidString(value) {
		return nil, errors.New("DUML packed string is not valid UTF-8")
	}
	if len(value) > 255 {
		return nil, errors.New("DUML packed string exceeds 255 bytes")
	}
	packed := make([]byte, len(value)+1)
	packed[0] = byte(len(value))
	copy(packed[1:], value)
	return packed, nil
}

func validSequenceOrder(order SequenceOrder) error {
	if order != SequenceBLE && order != SequenceUDP {
		return fmt.Errorf("unsupported DUML sequence order %d", order)
	}
	return nil
}

func decodeSequence(data []byte, order SequenceOrder) uint16 {
	if order == SequenceBLE {
		return binary.BigEndian.Uint16(data)
	}
	return binary.LittleEndian.Uint16(data)
}

func encodeSequence(data []byte, sequence uint16, order SequenceOrder) {
	if order == SequenceBLE {
		binary.BigEndian.PutUint16(data, sequence)
		return
	}
	binary.LittleEndian.PutUint16(data, sequence)
}

func crc8(data []byte) byte {
	crc := byte(0x77)
	for _, value := range data {
		crc ^= value
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8c
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func crc16(data []byte) uint16 {
	crc := uint16(0x3692)
	for _, value := range data {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}
