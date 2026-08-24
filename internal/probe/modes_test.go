package probe

import (
	"bytes"
	"strings"
	"testing"
)

func TestNamesObservedNonStorageActionModeInsteadOfSilence(t *testing.T) {
	usb := []USBDevice{{
		SysName: "3-2", VendorID: "2ca3", ProductID: "0025", Product: "OsmoAction6_SN:suffix",
		Manufacturer: "DJI", Serial: "123456789ABCDEF",
	}}

	if got := correlateSources(usb, nil, nil); len(got) != 0 {
		t.Fatalf("non-storage mode must not correlate a Source: %#v", got)
	}
	observations := observeCameraModes(usb)
	if len(observations) != 1 {
		t.Fatalf("expected one mode observation, got %#v", observations)
	}
	got := observations[0]
	if got.CameraModel != "DJI Osmo Action 6" || !strings.Contains(got.Mode, "2ca3:0025") {
		t.Fatalf("unexpected observation: %#v", got)
	}
	if got.Guidance == "" || got.Evidence == "" {
		t.Fatalf("observation must tell the operator what to do and cite evidence: %#v", got)
	}
	if strings.Contains(got.Reason+got.Guidance+got.Evidence+got.Mode, "123456789ABCDEF") {
		t.Fatalf("placeholder serial must not be reported as identity: %#v", got)
	}
}

func TestReportsRecognizedStorageIDWithoutMassStorageInterface(t *testing.T) {
	usb := []USBDevice{{
		SysName: "7-1.4", VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial",
		Interfaces: []USBInterface{{SysName: "7-1.4:1.0", Class: "ff", Subclass: "ff", Protocol: "30"}},
	}}
	observations := observeCameraModes(usb)
	if len(observations) != 1 || observations[0].CameraModel != "DJI Osmo Action 6" {
		t.Fatalf("expected a recognized-camera observation, got %#v", observations)
	}
}

func TestUnknownAndFullyCorrelatedDevicesProduceNoModeObservation(t *testing.T) {
	usb := []USBDevice{
		{SysName: "1-1", VendorID: "ffff", ProductID: "ffff"},
		{SysName: "7-1.4", VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial",
			Interfaces: []USBInterface{{Class: "08", Subclass: "06", Protocol: "50"}}},
	}
	if got := observeCameraModes(usb); len(got) != 0 {
		t.Fatalf("expected no observations, got %#v", got)
	}
}

func TestTextReportNamesNonStorageMode(t *testing.T) {
	var buffer bytes.Buffer
	report := Report{MTP: MTPEvidence{Tools: map[string]string{}}, CameraModes: observeCameraModes([]USBDevice{{
		SysName: "3-2", VendorID: "2ca3", ProductID: "0025",
	}})}
	if err := writeTextReport(&buffer, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	text := buffer.String()
	if !strings.Contains(text, "Recognized cameras without a Source (1)") || !strings.Contains(text, "2ca3:0025") {
		t.Fatalf("report does not name the known non-storage mode:\n%s", text)
	}
}
