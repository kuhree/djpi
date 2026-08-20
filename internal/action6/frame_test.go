package action6

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestFramesMatchDJIGoldenVectors(t *testing.T) {
	vectors := []string{
		"aa330002000000000100982f00197856341206383456789abc00000000000000000000000000000000d20400000000a70f381b",
		"aa420000000000000200d34d00176e2735019d2e0200009749018023ad1810a4000000004841000050c000000000e8030000e80300000a0000000b000000fe2937c4",
		"aa180000000000000300507e1d05021400000000fa4a4931",
	}
	for _, vector := range vectors {
		encoded := decodeFrameHex(t, vector)
		frame, err := ParseFrame(encoded)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := frame.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(roundTrip, encoded) {
			t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
		}
	}
	statusFrame, err := StatusSubscription(3).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(statusFrame); got != vectors[2] {
		t.Fatalf("status subscription = %s", got)
	}
}

func TestParseFrameFailsClosed(t *testing.T) {
	valid := decodeFrameHex(t, "aa180000000000000300507e1d05021400000000fa4a4931")
	for name, frame := range map[string][]byte{
		"short":        valid[:17],
		"trailing":     append(append([]byte(nil), valid...), 0),
		"start":        mutateFrame(valid, 0),
		"header crc":   mutateFrame(valid, 10),
		"frame crc":    mutateFrame(valid, 15),
		"encrypted":    mutateFrame(valid, 4),
		"reserved":     mutateFrame(valid, 5),
		"command bits": mutateFrame(valid, 3),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseFrame(frame); err == nil {
				t.Fatal("expected malformed R SDK frame refusal")
			}
		})
	}
}

func decodeFrameHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mutateFrame(value []byte, position int) []byte {
	mutated := append([]byte(nil), value...)
	mutated[position] ^= 0x80
	return mutated
}
