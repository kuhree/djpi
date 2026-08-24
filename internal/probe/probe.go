// Package probe collects read-only Linux evidence about connected camera sources.
package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const usbRoot = "/sys/bus/usb/devices"

// Report is a machine-readable snapshot of evidence visible to the local Linux host.
// It intentionally makes no claims about camera identity from unverified USB IDs.
type Report struct {
	USB              []USBDevice             `json:"usb"`
	Block            []BlockDevice           `json:"block"`
	Filesystems      []Filesystem            `json:"filesystems"`
	MTP              MTPEvidence             `json:"mtp"`
	SourceCandidates []SourceCandidate       `json:"source_candidates,omitempty"`
	CameraModes      []CameraModeObservation `json:"camera_modes,omitempty"`
	Warnings         []string                `json:"warnings,omitempty"`
}

type USBDevice struct {
	SysName      string         `json:"sys_name"`
	VendorID     string         `json:"vendor_id"`
	ProductID    string         `json:"product_id"`
	Manufacturer string         `json:"manufacturer,omitempty"`
	Product      string         `json:"product,omitempty"`
	Serial       string         `json:"serial,omitempty"`
	Class        string         `json:"class,omitempty"`
	Subclass     string         `json:"subclass,omitempty"`
	Protocol     string         `json:"protocol,omitempty"`
	Interfaces   []USBInterface `json:"interfaces,omitempty"`
}

type USBInterface struct {
	SysName  string `json:"sys_name"`
	Class    string `json:"class,omitempty"`
	Subclass string `json:"subclass,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

type BlockDevice struct {
	Name        string        `json:"name"`
	Path        string        `json:"path,omitempty"`
	Type        string        `json:"type,omitempty"`
	Size        int64         `json:"size_bytes,omitempty"`
	Removable   bool          `json:"removable"`
	ReadOnly    bool          `json:"read_only"`
	Transport   string        `json:"transport,omitempty"`
	Filesystem  string        `json:"filesystem,omitempty"`
	Label       string        `json:"label,omitempty"`
	UUID        string        `json:"uuid,omitempty"`
	Mountpoints []string      `json:"mountpoints,omitempty"`
	Vendor      string        `json:"vendor,omitempty"`
	Model       string        `json:"model,omitempty"`
	Serial      string        `json:"serial,omitempty"`
	WWN         string        `json:"wwn,omitempty"`
	Children    []BlockDevice `json:"children,omitempty"`
}

type Filesystem struct {
	Target   string       `json:"target"`
	Source   string       `json:"source"`
	Type     string       `json:"type,omitempty"`
	Options  string       `json:"options,omitempty"`
	Children []Filesystem `json:"children,omitempty"`
}

type MTPEvidence struct {
	PotentialInterfaces []string          `json:"potential_interfaces,omitempty"`
	MountedFilesystems  []string          `json:"mounted_filesystems,omitempty"`
	Tools               map[string]string `json:"tools"`
}

type fileReader interface {
	ReadDir(string) ([]fs.DirEntry, error)
	ReadFile(string) ([]byte, error)
}

type commandRunner interface {
	LookPath(string) (string, error)
	Output(string, ...string) ([]byte, error)
}

type osFiles struct{}

func (osFiles) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }
func (osFiles) ReadFile(name string) ([]byte, error)       { return os.ReadFile(name) }

type osCommands struct{}

func (osCommands) LookPath(file string) (string, error) { return exec.LookPath(file) }
func (osCommands) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// Collector keeps host interactions behind narrow boundaries for fixture testing.
type Collector struct {
	files    fileReader
	commands commandRunner
}

func NewSystemCollector() Collector {
	return Collector{files: osFiles{}, commands: osCommands{}}
}

func (c Collector) Collect() Report {
	report := Report{MTP: MTPEvidence{Tools: make(map[string]string)}}

	var err error
	report.USB, err = c.collectUSB()
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("USB sysfs: %v", err))
	}

	report.Block, err = c.collectBlock()
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("block devices: %v", err))
	}

	report.Filesystems, err = c.collectFilesystems()
	if err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("filesystems: %v", err))
	}

	report.MTP = c.collectMTP(report.USB, report.Filesystems)
	report.SourceCandidates = correlateSources(report.USB, report.Block, report.Filesystems)
	report.CameraModes = observeCameraModes(report.USB)
	sort.Strings(report.Warnings)
	return report
}

func (c Collector) collectUSB() ([]USBDevice, error) {
	entries, err := c.files.ReadDir(usbRoot)
	if err != nil {
		return nil, err
	}

	devices := make([]USBDevice, 0)
	for _, entry := range entries {
		base := filepath.Join(usbRoot, entry.Name())
		vendor, vendorErr := readTrimmed(c.files, filepath.Join(base, "idVendor"))
		product, productErr := readTrimmed(c.files, filepath.Join(base, "idProduct"))
		if vendorErr != nil || productErr != nil {
			continue
		}
		device := USBDevice{
			SysName: entry.Name(), VendorID: vendor, ProductID: product,
			Manufacturer: optionalFile(c.files, base, "manufacturer"),
			Product:      optionalFile(c.files, base, "product"),
			Serial:       optionalFile(c.files, base, "serial"),
			Class:        optionalFile(c.files, base, "bDeviceClass"),
			Subclass:     optionalFile(c.files, base, "bDeviceSubClass"),
			Protocol:     optionalFile(c.files, base, "bDeviceProtocol"),
		}
		device.Interfaces = c.usbInterfaces(entry.Name(), entries)
		devices = append(devices, device)
	}
	return devices, nil
}

func (c Collector) usbInterfaces(deviceName string, entries []fs.DirEntry) []USBInterface {
	prefix := deviceName + ":"
	result := make([]USBInterface, 0)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		base := filepath.Join(usbRoot, entry.Name())
		result = append(result, USBInterface{
			SysName:  entry.Name(),
			Class:    optionalFile(c.files, base, "bInterfaceClass"),
			Subclass: optionalFile(c.files, base, "bInterfaceSubClass"),
			Protocol: optionalFile(c.files, base, "bInterfaceProtocol"),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SysName < result[j].SysName })
	return result
}

func (c Collector) collectBlock() ([]BlockDevice, error) {
	if _, err := c.commands.LookPath("lsblk"); err != nil {
		return nil, errors.New("lsblk is not available")
	}
	out, err := c.commands.Output("lsblk", "--json", "--bytes", "--output", "NAME,PATH,TYPE,SIZE,RM,RO,TRAN,FSTYPE,LABEL,UUID,MOUNTPOINTS,VENDOR,MODEL,SERIAL,WWN")
	if err != nil {
		return nil, err
	}
	// lsblk uses abbreviated field names which intentionally differ from the
	// public report's descriptive JSON field names.
	type wireBlock struct {
		Name        string      `json:"name"`
		Path        string      `json:"path"`
		Type        string      `json:"type"`
		Size        int64       `json:"size"`
		Removable   bool        `json:"rm"`
		ReadOnly    bool        `json:"ro"`
		Transport   string      `json:"tran"`
		Filesystem  string      `json:"fstype"`
		Label       string      `json:"label"`
		UUID        string      `json:"uuid"`
		Mountpoints []*string   `json:"mountpoints"`
		Vendor      string      `json:"vendor"`
		Model       string      `json:"model"`
		Serial      string      `json:"serial"`
		WWN         string      `json:"wwn"`
		Children    []wireBlock `json:"children"`
	}
	var envelope struct {
		Devices []wireBlock `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	var convert func(wireBlock) BlockDevice
	convert = func(raw wireBlock) BlockDevice {
		device := BlockDevice{Name: raw.Name, Path: raw.Path, Type: raw.Type, Size: raw.Size, Removable: raw.Removable,
			ReadOnly: raw.ReadOnly, Transport: raw.Transport, Filesystem: raw.Filesystem, Label: raw.Label, UUID: raw.UUID,
			Vendor: strings.TrimSpace(raw.Vendor), Model: strings.TrimSpace(raw.Model), Serial: strings.TrimSpace(raw.Serial), WWN: raw.WWN}
		for _, mountpoint := range raw.Mountpoints {
			if mountpoint != nil {
				device.Mountpoints = append(device.Mountpoints, *mountpoint)
			}
		}
		for _, child := range raw.Children {
			device.Children = append(device.Children, convert(child))
		}
		return device
	}
	devices := make([]BlockDevice, 0, len(envelope.Devices))
	for _, raw := range envelope.Devices {
		devices = append(devices, convert(raw))
	}
	return devices, nil
}

func (c Collector) collectFilesystems() ([]Filesystem, error) {
	if _, err := c.commands.LookPath("findmnt"); err != nil {
		return nil, errors.New("findmnt is not available")
	}
	out, err := c.commands.Output("findmnt", "--json", "--real", "--output", "TARGET,SOURCE,FSTYPE,OPTIONS")
	if err != nil {
		return nil, err
	}
	type wireFilesystem struct {
		Target   string           `json:"target"`
		Source   string           `json:"source"`
		Type     string           `json:"fstype"`
		Options  string           `json:"options"`
		Children []wireFilesystem `json:"children"`
	}
	var envelope struct {
		Filesystems []wireFilesystem `json:"filesystems"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, err
	}
	var convert func(wireFilesystem) Filesystem
	convert = func(raw wireFilesystem) Filesystem {
		item := Filesystem{Target: raw.Target, Source: raw.Source, Type: raw.Type, Options: raw.Options}
		for _, child := range raw.Children {
			item.Children = append(item.Children, convert(child))
		}
		return item
	}
	filesystems := make([]Filesystem, 0, len(envelope.Filesystems))
	for _, raw := range envelope.Filesystems {
		filesystems = append(filesystems, convert(raw))
	}
	return filesystems, nil
}

func (c Collector) collectMTP(devices []USBDevice, filesystems []Filesystem) MTPEvidence {
	evidence := MTPEvidence{Tools: make(map[string]string)}
	for _, device := range devices {
		for _, iface := range device.Interfaces {
			// USB imaging/PTP interfaces are relevant evidence, not proof of MTP.
			if strings.EqualFold(iface.Class, "06") {
				evidence.PotentialInterfaces = append(evidence.PotentialInterfaces, iface.SysName)
			}
		}
	}
	walkFilesystems(filesystems, func(item Filesystem) {
		value := strings.ToLower(item.Type + " " + item.Source + " " + item.Target)
		if strings.Contains(value, "mtp") || strings.Contains(value, "gvfs") {
			evidence.MountedFilesystems = append(evidence.MountedFilesystems, item.Target+" ("+item.Type+")")
		}
	})
	for _, tool := range []string{"gio", "mtp-detect", "jmtpfs", "simple-mtpfs"} {
		path, err := c.commands.LookPath(tool)
		if err != nil {
			evidence.Tools[tool] = "unavailable"
		} else {
			evidence.Tools[tool] = path
		}
	}
	sort.Strings(evidence.PotentialInterfaces)
	sort.Strings(evidence.MountedFilesystems)
	return evidence
}

func readTrimmed(files fileReader, path string) (string, error) {
	value, err := files.ReadFile(path)
	return strings.TrimSpace(string(value)), err
}

func optionalFile(files fileReader, base, name string) string {
	value, _ := readTrimmed(files, filepath.Join(base, name))
	return value
}

func walkFilesystems(items []Filesystem, visit func(Filesystem)) {
	for _, item := range items {
		visit(item)
		walkFilesystems(item.Children, visit)
	}
}

// WriteReport renders either stable JSON evidence or a concise operator report.
func WriteReport(w io.Writer, report Report, jsonOutput bool) error {
	if jsonOutput {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return writeTextReport(w, report)
}
