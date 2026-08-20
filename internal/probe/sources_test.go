package probe

import (
	"strings"
	"testing"
)

func TestCorrelatesHardwareObservedAction6MassStorage(t *testing.T) {
	usb := []USBDevice{{
		SysName: "7-1.4", VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial",
		Interfaces: []USBInterface{
			{SysName: "7-1.4:1.0", Class: "ff", Subclass: "ff", Protocol: "30"},
			{SysName: "7-1.4:1.1", Class: "08", Subclass: "06", Protocol: "50"},
		},
	}}
	blocks := []BlockDevice{
		{Name: "sdd", Path: "/dev/sdd", Size: 54_028_767_232, Removable: true, Transport: "usb", Filesystem: "exfat", Label: "OsmoAction", Serial: "camera-serial", Mountpoints: []string{"/run/media/operator/OsmoAction"}},
		{Name: "sde", Path: "/dev/sde", Size: 0, Removable: true, Transport: "usb", Serial: "camera-serial"},
		{Name: "sdf", Path: "/dev/sdf", Size: 8_000_000, Removable: true, Transport: "usb", Filesystem: "vfat", Serial: "different-device"},
	}
	filesystems := []Filesystem{{Target: "/", Source: "/dev/root", Type: "btrfs", Children: []Filesystem{{
		Target: "/run/media/operator/OsmoAction", Source: "/dev/sdd", Type: "exfat", Options: "rw,nosuid,noatime,errors=remount-ro",
	}}}}

	candidates := correlateSources(usb, blocks, filesystems)
	if len(candidates) != 1 {
		t.Fatalf("expected one source candidate, got %#v", candidates)
	}
	got := candidates[0]
	if got.CameraModel != "DJI Osmo Action 6" || got.BlockPath != "/dev/sdd" || got.Mountpoint != "/run/media/operator/OsmoAction" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
	if got.CameraID == "" || strings.Contains(got.CameraID, "camera-serial") {
		t.Fatalf("unsafe camera identity: %q", got.CameraID)
	}
	if got.MountReadOnly {
		t.Fatalf("errors=remount-ro must not be mistaken for a read-only mount: %#v", got)
	}
	if strings.Contains(got.IdentityEvidence, "camera-serial") {
		t.Fatalf("identity evidence must not expose the camera serial: %q", got.IdentityEvidence)
	}
}

func TestCameraIdentitySeparatesPhysicalCamerasWithoutExposingSerial(t *testing.T) {
	first := cameraID("DJI Osmo Pocket 3", "serial-one")
	second := cameraID("DJI Osmo Pocket 3", "serial-two")
	if first == second || strings.Contains(first, "serial-one") || strings.Contains(second, "serial-two") {
		t.Fatalf("unsafe camera IDs: %q %q", first, second)
	}
}

func TestCorrelationRequiresKnownIDMassStorageAndMatchingSerial(t *testing.T) {
	block := []BlockDevice{{Path: "/dev/sdd", Size: 42, Transport: "usb", Filesystem: "exfat", Serial: "camera-serial"}}
	tests := []USBDevice{
		{VendorID: "ffff", ProductID: "ffff", Serial: "camera-serial", Interfaces: []USBInterface{{Class: "08", Subclass: "06", Protocol: "50"}}},
		{VendorID: "2ca3", ProductID: "8002", Serial: "different-serial", Interfaces: []USBInterface{{Class: "08", Subclass: "06", Protocol: "50"}}},
		{VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial", Interfaces: []USBInterface{{Class: "ff", Subclass: "ff", Protocol: "30"}}},
	}
	for _, usb := range tests {
		if got := correlateSources([]USBDevice{usb}, block, nil); len(got) != 0 {
			t.Fatalf("unsafe correlation for %#v: %#v", usb, got)
		}
	}
}

func TestCorrelatesHardwareObservedPocket3MassStorage(t *testing.T) {
	usb := []USBDevice{{
		SysName: "7-1.4", VendorID: "2ca3", ProductID: "0020", Serial: "pocket-serial",
		Interfaces: []USBInterface{
			{Class: "e0", Subclass: "01", Protocol: "03"},
			{Class: "0a", Subclass: "00", Protocol: "00"},
			{Class: "08", Subclass: "06", Protocol: "50"},
			{Class: "ff", Subclass: "43", Protocol: "01"},
		},
	}}
	blocks := []BlockDevice{{Path: "/dev/sdd", Size: 127_982_895_104, Transport: "usb", Filesystem: "exfat", Serial: "pocket-serial", Mountpoints: []string{"/camera"}}}
	candidates := correlateSources(usb, blocks, []Filesystem{{Target: "/camera", Source: "/dev/sdd", Type: "exfat", Options: "rw,noatime"}})
	if len(candidates) != 1 || candidates[0].CameraModel != "DJI Osmo Pocket 3" || candidates[0].BlockPath != "/dev/sdd" {
		t.Fatalf("unexpected Pocket 3 correlation: %#v", candidates)
	}
}

func TestCorrelationInheritsParentBlockSerialAndTransport(t *testing.T) {
	usb := []USBDevice{{VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial", Interfaces: []USBInterface{{Class: "08", Subclass: "06", Protocol: "50"}}}}
	blocks := []BlockDevice{{Path: "/dev/sdd", Serial: "camera-serial", Transport: "usb", Children: []BlockDevice{{Path: "/dev/sdd1", Size: 42, Filesystem: "exfat", Mountpoints: []string{"/camera"}}}}}
	candidates := correlateSources(usb, blocks, []Filesystem{{Target: "/camera", Options: "ro,nosuid"}})
	if len(candidates) != 1 || candidates[0].BlockPath != "/dev/sdd1" || !candidates[0].MountReadOnly {
		t.Fatalf("unexpected inherited correlation: %#v", candidates)
	}
}

func TestSingleMountedSourceFailsClosed(t *testing.T) {
	if _, err := SingleMountedSource(nil); err == nil {
		t.Fatal("expected no-source error")
	}
	if _, err := SingleMountedSource([]SourceCandidate{{BlockPath: "/dev/sdd"}}); err == nil || !strings.Contains(err.Error(), "will not mount") {
		t.Fatalf("expected unmounted error, got %v", err)
	}
	if _, err := SingleMountedSource([]SourceCandidate{{Mountpoint: "/camera-one"}, {Mountpoint: "/camera-two"}}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	want := SourceCandidate{Mountpoint: "/camera"}
	if got, err := SingleMountedSource([]SourceCandidate{want}); err != nil || got.Mountpoint != want.Mountpoint {
		t.Fatalf("expected one mounted source, got %#v, %v", got, err)
	}
}

func TestMultipleMountpointsRemainAmbiguous(t *testing.T) {
	usb := []USBDevice{{VendorID: "2ca3", ProductID: "8002", Serial: "camera-serial", Interfaces: []USBInterface{{Class: "08", Subclass: "06", Protocol: "50"}}}}
	blocks := []BlockDevice{{Path: "/dev/sdd", Serial: "camera-serial", Transport: "usb", Size: 42, Filesystem: "exfat", Mountpoints: []string{"/camera-b", "/camera-a"}}}
	candidates := correlateSources(usb, blocks, nil)
	if len(candidates) != 2 || candidates[0].Mountpoint != "/camera-a" || candidates[1].Mountpoint != "/camera-b" {
		t.Fatalf("expected both sorted mountpoints, got %#v", candidates)
	}
	if _, err := SingleMountedSource(candidates); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected multiple mountpoints to fail closed, got %v", err)
	}
}
