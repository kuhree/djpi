package action6

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type CameraStatus struct {
	Mode           byte   `json:"mode"`
	Status         byte   `json:"status"`
	Resolution     byte   `json:"resolution"`
	FPSIndex       byte   `json:"fps_index"`
	RecordSeconds  uint16 `json:"record_seconds"`
	BatteryPercent byte   `json:"battery_percent"`
	Recording      bool   `json:"recording"`
}

type ModeInfo struct {
	Name      string `json:"name"`
	Parameter string `json:"parameter"`
}

// DecodeCameraStatus decodes the stable fields from a 0x1d/0x02 status push.
func DecodeCameraStatus(frame Frame) (CameraStatus, error) {
	if frame.CommandSet != 0x1d || frame.CommandID != 0x02 {
		return CameraStatus{}, errors.New("R SDK frame is not a camera status push")
	}
	if len(frame.Payload) < 7 {
		return CameraStatus{}, errors.New("R SDK camera status payload is too short")
	}
	status := CameraStatus{
		Mode:           frame.Payload[0],
		Status:         frame.Payload[1],
		Resolution:     frame.Payload[2],
		FPSIndex:       frame.Payload[3],
		RecordSeconds:  binary.LittleEndian.Uint16(frame.Payload[5:7]),
		BatteryPercent: frame.Payload[len(frame.Payload)-1],
	}
	if status.BatteryPercent > 100 {
		return CameraStatus{}, fmt.Errorf("invalid R SDK battery percentage %d", status.BatteryPercent)
	}
	status.Recording = status.Status == 0x03 || status.Status == 0x05
	return status, nil
}

// DecodeModeInfo decodes Action 6's text mode 0x1d/0x06 status push.
func DecodeModeInfo(frame Frame) (ModeInfo, error) {
	if frame.CommandSet != 0x1d || frame.CommandID != 0x06 {
		return ModeInfo{}, errors.New("R SDK frame is not a text mode status push")
	}
	payload := frame.Payload
	if len(payload) != 46 || payload[0] != 0x01 || payload[23] != 0x02 {
		return ModeInfo{}, errors.New("R SDK text mode payload has an unsupported layout")
	}
	name, err := fixedASCII(payload[2:23], int(payload[1]))
	if err != nil {
		return ModeInfo{}, fmt.Errorf("decode R SDK mode name: %w", err)
	}
	parameter, err := fixedASCII(payload[25:46], int(payload[24]))
	if err != nil {
		return ModeInfo{}, fmt.Errorf("decode R SDK mode parameter: %w", err)
	}
	return ModeInfo{Name: name, Parameter: parameter}, nil
}

func fixedASCII(field []byte, length int) (string, error) {
	if length < 0 || length > 20 || length > len(field) {
		return "", errors.New("declared ASCII field length is invalid")
	}
	for _, value := range field[:length] {
		if value < 0x20 || value > 0x7e {
			return "", errors.New("ASCII field contains a non-printable byte")
		}
	}
	return strings.TrimSpace(string(field[:length])), nil
}
