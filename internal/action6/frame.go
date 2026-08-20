// Package action6 contains transport-independent DJI R SDK protocol pieces for
// the Osmo Action 6. It performs no Bluetooth or camera I/O.
package action6

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	rsdkSOF             = 0xaa
	rsdkVersion         = 0
	rsdkMinimum         = 18
	rsdkMaximum         = 0x3ff
	CommandNoReply byte = 0x00
	CommandWait    byte = 0x02
	ReplyNoReply   byte = 0x20
)

// Frame is one CRC-validated, unencrypted DJI R SDK message. Encryption and
// reserved header variants are refused until independently characterized.
type Frame struct {
	CommandType byte
	Sequence    uint16
	CommandSet  byte
	CommandID   byte
	Payload     []byte
}

func ParseFrame(data []byte) (Frame, error) {
	if len(data) < rsdkMinimum {
		return Frame{}, fmt.Errorf("R SDK frame is %d bytes; minimum is %d", len(data), rsdkMinimum)
	}
	if data[0] != rsdkSOF {
		return Frame{}, fmt.Errorf("R SDK frame has invalid start byte %#02x", data[0])
	}
	versionLength := binary.LittleEndian.Uint16(data[1:3])
	if version := versionLength >> 10; version != rsdkVersion {
		return Frame{}, fmt.Errorf("unsupported R SDK frame version %d", version)
	}
	length := int(versionLength & 0x03ff)
	if length != len(data) {
		return Frame{}, fmt.Errorf("R SDK frame declares %d bytes but received %d", length, len(data))
	}
	if data[3]&0xc0 != 0 {
		return Frame{}, fmt.Errorf("R SDK command type has unsupported reserved bits %#02x", data[3])
	}
	if data[4] != 0 {
		return Frame{}, errors.New("encrypted R SDK frames are not supported")
	}
	if data[5] != 0 || data[6] != 0 || data[7] != 0 {
		return Frame{}, errors.New("R SDK frame has nonzero reserved header bytes")
	}
	if actual, wanted := rsdkCRC16(data[:10]), binary.LittleEndian.Uint16(data[10:12]); actual != wanted {
		return Frame{}, fmt.Errorf("R SDK header CRC mismatch: got %#04x, want %#04x", wanted, actual)
	}
	dataEnd := len(data) - 4
	if actual, wanted := rsdkCRC32(data[:dataEnd]), binary.LittleEndian.Uint32(data[dataEnd:]); actual != wanted {
		return Frame{}, fmt.Errorf("R SDK frame CRC mismatch: got %#08x, want %#08x", wanted, actual)
	}
	return Frame{
		CommandType: data[3],
		Sequence:    binary.LittleEndian.Uint16(data[8:10]),
		CommandSet:  data[12],
		CommandID:   data[13],
		Payload:     append([]byte(nil), data[14:dataEnd]...),
	}, nil
}

func (frame Frame) Encode() ([]byte, error) {
	length := rsdkMinimum + len(frame.Payload)
	if length > rsdkMaximum {
		return nil, fmt.Errorf("R SDK frame length %d exceeds %d-byte protocol maximum", length, rsdkMaximum)
	}
	if frame.CommandType&0xc0 != 0 {
		return nil, fmt.Errorf("R SDK command type has unsupported reserved bits %#02x", frame.CommandType)
	}
	encoded := make([]byte, length)
	encoded[0] = rsdkSOF
	binary.LittleEndian.PutUint16(encoded[1:3], uint16(length)|rsdkVersion<<10)
	encoded[3] = frame.CommandType
	binary.LittleEndian.PutUint16(encoded[8:10], frame.Sequence)
	binary.LittleEndian.PutUint16(encoded[10:12], rsdkCRC16(encoded[:10]))
	encoded[12], encoded[13] = frame.CommandSet, frame.CommandID
	copy(encoded[14:], frame.Payload)
	binary.LittleEndian.PutUint32(encoded[length-4:], rsdkCRC32(encoded[:length-4]))
	return encoded, nil
}

// StatusSubscription builds the official periodic 2 Hz status request as
// inert protocol data. No transport is present in this package to send it.
func StatusSubscription(sequence uint16) Frame {
	return Frame{
		CommandType: CommandNoReply,
		Sequence:    sequence,
		CommandSet:  0x1d,
		CommandID:   0x05,
		Payload:     []byte{0x02, 0x14, 0, 0, 0, 0},
	}
}

func rsdkCRC16(data []byte) uint16 {
	crc := uint16(0x3aa3)
	for _, value := range data {
		crc ^= uint16(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func rsdkCRC32(data []byte) uint32 {
	crc := uint32(0x3aa3)
	for _, value := range data {
		crc ^= uint32(value)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}
