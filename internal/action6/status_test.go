package action6

import "testing"

func TestDecodeCameraStatus(t *testing.T) {
	payload := make([]byte, 46)
	payload[0], payload[1], payload[2], payload[3] = 0x01, 0x03, 103, 3
	payload[5], payload[45] = 5, 77
	status, err := DecodeCameraStatus(Frame{CommandSet: 0x1d, CommandID: 0x02, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != 1 || !status.Recording || status.RecordSeconds != 5 || status.BatteryPercent != 77 {
		t.Fatalf("status = %#v", status)
	}
	payload[45] = 101
	if _, err := DecodeCameraStatus(Frame{CommandSet: 0x1d, CommandID: 0x02, Payload: payload}); err == nil {
		t.Fatal("expected invalid battery refusal")
	}
}

func TestDecodeAction6TextMode(t *testing.T) {
	payload := make([]byte, 46)
	payload[0], payload[1] = 0x01, 5
	copy(payload[2:], "VIDEO")
	payload[23], payload[24] = 0x02, 4
	copy(payload[25:], "4K30")
	mode, err := DecodeModeInfo(Frame{CommandSet: 0x1d, CommandID: 0x06, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if mode.Name != "VIDEO" || mode.Parameter != "4K30" {
		t.Fatalf("mode = %#v", mode)
	}
	payload[1] = 21
	if _, err := DecodeModeInfo(Frame{CommandSet: 0x1d, CommandID: 0x06, Payload: payload}); err == nil {
		t.Fatal("expected oversized mode name refusal")
	}
}
