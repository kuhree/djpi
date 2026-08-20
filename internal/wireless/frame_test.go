package wireless

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestParseCapturedFrames(t *testing.T) {
	tests := []struct {
		name       string
		hex        string
		order      SequenceOrder
		sequence   uint16
		commandSet byte
		commandID  byte
		payload    string
	}{
		{name: "enter playback", hex: "55110492020100a040020c01010001b63b", order: SequenceBLE, sequence: 0x00a0, commandSet: 0x02, commandID: 0x0c, payload: "01010001"},
		{name: "media list request", hex: "553704f9020100a04000264a002a10010000000000010000002d000d0100ffffffffffffffff0001000000000000000000000000008185", order: SequenceBLE, sequence: 0x00a0, commandSet: 0x00, commandID: 0x26, payload: "4a002a10010000000000010000002d000d0100ffffffffffffffff000100000000000000000000000000"},
		{name: "captured session ping", hex: "550f04a202f01bcb40002b04009ab9", order: SequenceBLE, sequence: 0x1bcb, commandSet: 0x00, commandID: 0x2b, payload: "0400"},
		{name: "captured 53/10", hex: "55110492021c1dcb40531000000000894a", order: SequenceBLE, sequence: 0x1dcb, commandSet: 0x53, commandID: 0x10, payload: "00000000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := decodeHex(t, test.hex)
			frame, err := ParseFrame(encoded, test.order)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Sequence != test.sequence || frame.CommandSet != test.commandSet || frame.CommandID != test.commandID {
				t.Fatalf("decoded frame = %#v", frame)
			}
			if got := hex.EncodeToString(frame.Payload); got != test.payload {
				t.Fatalf("payload = %s, want %s", got, test.payload)
			}
			roundTrip, err := frame.Encode(test.order)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(roundTrip, encoded) {
				t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
			}
		})
	}
}

func TestSequenceByteOrderIsExplicit(t *testing.T) {
	frame := Frame{Sender: 2, Receiver: 1, Sequence: 0x1234, Flags: 0x40, CommandSet: 7, CommandID: 0x45}
	ble, err := frame.Encode(SequenceBLE)
	if err != nil {
		t.Fatal(err)
	}
	udp, err := frame.Encode(SequenceUDP)
	if err != nil {
		t.Fatal(err)
	}
	if got := ble[6:8]; !bytes.Equal(got, []byte{0x12, 0x34}) {
		t.Fatalf("BLE sequence bytes = %x", got)
	}
	if got := udp[6:8]; !bytes.Equal(got, []byte{0x34, 0x12}) {
		t.Fatalf("UDP sequence bytes = %x", got)
	}
	if _, err := ParseFrame(ble, SequenceUDP); err != nil {
		t.Fatal(err)
	}
}

func TestLongFrameUsesTenBitLength(t *testing.T) {
	payload := bytes.Repeat([]byte{0xa5}, 300)
	encoded, err := (Frame{Payload: payload}).Encode(SequenceUDP)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[2] != 0x05 {
		t.Fatalf("length/version high byte = %#02x, want 0x05", encoded[2])
	}
	decoded, err := ParseFrame(encoded, SequenceUDP)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("long payload did not round trip")
	}
}

func TestParseFrameFailsClosed(t *testing.T) {
	valid := decodeHex(t, "55110492020100a040020c01010001b63b")
	tests := map[string][]byte{
		"short":          valid[:12],
		"trailing":       append(append([]byte(nil), valid...), 0),
		"bad start":      mutate(valid, 0),
		"bad header crc": mutate(valid, 3),
		"bad frame crc":  mutate(valid, 11),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFrame(data, SequenceBLE); err == nil {
				t.Fatal("expected malformed frame refusal")
			}
		})
	}
	if _, err := ParseFrame(valid, SequenceOrder(99)); err == nil {
		t.Fatal("expected unknown sequence order refusal")
	}
}

func TestCRCAndPackStringVectors(t *testing.T) {
	if got := crc8([]byte{0x55, 0x33, 0x04}); got != 0xc2 {
		t.Fatalf("CRC8 = %#02x, want 0xc2", got)
	}
	packed, err := PackString("djpi")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packed, []byte{4, 'd', 'j', 'p', 'i'}) {
		t.Fatalf("packed string = %x", packed)
	}
	if _, err := PackString(strings.Repeat("x", 256)); err == nil {
		t.Fatal("expected oversized string refusal")
	}
	if _, err := PackString(string([]byte{0xff})); err == nil {
		t.Fatal("expected invalid UTF-8 refusal")
	}
}

func decodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mutate(value []byte, position int) []byte {
	mutated := append([]byte(nil), value...)
	mutated[position] ^= 0x01
	return mutated
}
