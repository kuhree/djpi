package probe

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"
)

type fixtureFiles struct {
	files map[string][]byte
	dirs  map[string][]fs.DirEntry
}

func (f fixtureFiles) ReadDir(name string) ([]fs.DirEntry, error) {
	v, ok := f.dirs[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return v, nil
}
func (f fixtureFiles) ReadFile(name string) ([]byte, error) {
	v, ok := f.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return v, nil
}

type fixtureDir string

func (d fixtureDir) Name() string               { return string(d) }
func (fixtureDir) IsDir() bool                  { return true }
func (fixtureDir) Type() fs.FileMode            { return fs.ModeDir }
func (d fixtureDir) Info() (fs.FileInfo, error) { return fixtureInfo(d), nil }

type fixtureInfo string

func (i fixtureInfo) Name() string     { return string(i) }
func (fixtureInfo) Size() int64        { return 0 }
func (fixtureInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (fixtureInfo) ModTime() time.Time { return time.Time{} }
func (fixtureInfo) IsDir() bool        { return true }
func (fixtureInfo) Sys() any           { return nil }

type fixtureCommands struct {
	paths   map[string]string
	outputs map[string][]byte
}

func (f fixtureCommands) LookPath(name string) (string, error) {
	if path := f.paths[name]; path != "" {
		return path, nil
	}
	return "", errors.New("not found")
}
func (f fixtureCommands) Output(name string, _ ...string) ([]byte, error) {
	if output, ok := f.outputs[name]; ok {
		return output, nil
	}
	return nil, errors.New("unexpected command")
}

func TestCollectsUSBBlockFilesystemAndMTPEvidence(t *testing.T) {
	files := fixtureFiles{
		dirs: map[string][]fs.DirEntry{usbRoot: {fixtureDir("1-2"), fixtureDir("1-2:1.0")}},
		files: map[string][]byte{
			usbRoot + "/1-2/idVendor": []byte("1234\n"), usbRoot + "/1-2/idProduct": []byte("abcd\n"),
			usbRoot + "/1-2/manufacturer": []byte("Fixture Camera Co\n"), usbRoot + "/1-2/product": []byte("Camera Fixture\n"),
			usbRoot + "/1-2/serial": []byte("serial-1\n"), usbRoot + "/1-2/bDeviceClass": []byte("00\n"),
			usbRoot + "/1-2:1.0/bInterfaceClass": []byte("06\n"), usbRoot + "/1-2:1.0/bInterfaceSubClass": []byte("01\n"),
			usbRoot + "/1-2:1.0/bInterfaceProtocol": []byte("01\n"),
		},
	}
	commands := fixtureCommands{
		paths: map[string]string{"lsblk": "/bin/lsblk", "findmnt": "/bin/findmnt", "gio": "/bin/gio"},
		outputs: map[string][]byte{
			"lsblk":   []byte(`{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":64000000,"rm":true,"ro":true,"tran":"usb","vendor":" Fixture ","model":" Media ","children":[{"name":"sdb1","path":"/dev/sdb1","type":"part","fstype":"exfat","mountpoints":[null]}]}]}`),
			"findmnt": []byte(`{"filesystems":[{"target":"/","source":"/dev/root","fstype":"ext4","options":"ro","children":[{"target":"/run/user/1000/gvfs/mtp:host=fixture","source":"gvfsd-fuse","fstype":"fuse.gvfsd-fuse","options":"rw"}]}]}`),
		},
	}
	report := (Collector{files: files, commands: commands}).Collect()
	if len(report.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", report.Warnings)
	}
	if len(report.USB) != 1 || report.USB[0].VendorID != "1234" || report.USB[0].ProductID != "abcd" {
		t.Fatalf("unexpected USB evidence: %#v", report.USB)
	}
	if got := report.MTP.PotentialInterfaces; len(got) != 1 || got[0] != "1-2:1.0" {
		t.Fatalf("unexpected potential MTP interfaces: %v", got)
	}
	if len(report.MTP.MountedFilesystems) != 1 {
		t.Fatalf("expected mounted MTP evidence, got %v", report.MTP.MountedFilesystems)
	}
	if report.MTP.Tools["gio"] != "/bin/gio" || report.MTP.Tools["mtp-detect"] != "unavailable" {
		t.Fatalf("unexpected MTP tools: %v", report.MTP.Tools)
	}
	if len(report.Block) != 1 || !report.Block[0].Removable || !report.Block[0].ReadOnly || report.Block[0].Transport != "usb" || report.Block[0].Vendor != "Fixture" || report.Block[0].Model != "Media" || report.Block[0].Children[0].Filesystem != "exfat" {
		t.Fatalf("unexpected block evidence: %#v", report.Block)
	}
}

func TestMissingHostCapabilitiesBecomeWarnings(t *testing.T) {
	report := (Collector{files: fixtureFiles{}, commands: fixtureCommands{}}).Collect()
	if len(report.Warnings) != 3 {
		t.Fatalf("expected USB, block, and filesystem warnings, got %v", report.Warnings)
	}
	if report.MTP.Tools["gio"] != "unavailable" {
		t.Fatalf("expected unavailable MTP tools, got %v", report.MTP.Tools)
	}
}

func TestTextReportStatesSafetyAndIdentityLimits(t *testing.T) {
	var output strings.Builder
	if err := WriteReport(&output, Report{MTP: MTPEvidence{Tools: map[string]string{"gio": "unavailable"}}}, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"read-only host probe", "No devices are mounted, copied, deleted, or formatted", "Only repository hardware-observed USB IDs are recognized"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output did not contain %q:\n%s", want, output.String())
		}
	}
}
