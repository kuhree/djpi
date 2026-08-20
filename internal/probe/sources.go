package probe

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SourceCandidate is a non-destructive correlation between hardware-observed
// camera USB evidence and a Linux storage node. It is not camera enrollment.
type SourceCandidate struct {
	CameraModel      string `json:"camera_model"`
	CameraID         string `json:"camera_id"`
	IdentityEvidence string `json:"identity_evidence"`
	USBDevice        string `json:"usb_device"`
	BlockPath        string `json:"block_path"`
	SizeBytes        int64  `json:"size_bytes"`
	Filesystem       string `json:"filesystem"`
	Label            string `json:"label,omitempty"`
	BlockReadOnly    bool   `json:"block_read_only"`
	Mountpoint       string `json:"mountpoint,omitempty"`
	MountReadOnly    bool   `json:"mount_read_only"`
	MountOptions     string `json:"mount_options,omitempty"`
}

type observedCameraIdentity struct {
	model    string
	evidence string
}

// These IDs come only from repository hardware evidence. Keep mode and
// firmware stability caveats in the referenced evidence, and never guess IDs.
var observedCameraIdentities = map[string]observedCameraIdentity{
	"2ca3:8002": {
		model:    "DJI Osmo Action 6",
		evidence: "docs/hardware/action-6-usb.md (observed USB storage mode)",
	},
	"2ca3:0020": {
		model:    "DJI Osmo Pocket 3",
		evidence: "docs/hardware/pocket-3-usb.md (observed USB storage mode)",
	},
}

func correlateSources(usbDevices []USBDevice, blockDevices []BlockDevice, filesystems []Filesystem) []SourceCandidate {
	mounts := flattenMounts(filesystems)
	blocks := flattenBlocks(blockDevices, blockInheritance{})
	candidates := make([]SourceCandidate, 0)

	for _, usb := range usbDevices {
		identity, known := observedCameraIdentities[strings.ToLower(usb.VendorID+":"+usb.ProductID)]
		if !known || usb.Serial == "" || !hasMassStorageInterface(usb) {
			continue
		}
		for _, block := range blocks {
			if block.serial == "" || block.serial != usb.Serial || block.transport != "usb" || block.device.Size <= 0 || block.device.Filesystem == "" {
				continue
			}
			baseCandidate := SourceCandidate{
				CameraModel: identity.model, CameraID: cameraID(identity.model, usb.Serial), IdentityEvidence: identity.evidence,
				USBDevice: usb.SysName, BlockPath: block.device.Path, SizeBytes: block.device.Size,
				Filesystem: block.device.Filesystem, Label: block.device.Label, BlockReadOnly: block.device.ReadOnly,
			}
			if len(block.device.Mountpoints) == 0 {
				candidates = append(candidates, baseCandidate)
				continue
			}
			mountpoints := append([]string(nil), block.device.Mountpoints...)
			sort.Strings(mountpoints)
			for _, mountpoint := range mountpoints {
				candidate := baseCandidate
				candidate.Mountpoint = mountpoint
				if mount, ok := mounts[mountpoint]; ok {
					candidate.MountOptions = mount.Options
					candidate.MountReadOnly = hasMountOption(mount.Options, "ro")
				}
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CameraModel != candidates[j].CameraModel {
			return candidates[i].CameraModel < candidates[j].CameraModel
		}
		if candidates[i].BlockPath != candidates[j].BlockPath {
			return candidates[i].BlockPath < candidates[j].BlockPath
		}
		return candidates[i].Mountpoint < candidates[j].Mountpoint
	})
	return candidates
}

func cameraID(model, serial string) string {
	digest := sha256.Sum256([]byte(model + "\x00" + serial))
	return fmt.Sprintf("camera-sha256:%x", digest[:])
}

// SingleMountedSource fails closed unless exactly one correlated storage node
// has an existing mountpoint. It never mounts the node itself.
func SingleMountedSource(candidates []SourceCandidate) (SourceCandidate, error) {
	mounted := make([]SourceCandidate, 0)
	for _, candidate := range candidates {
		if candidate.Mountpoint != "" {
			mounted = append(mounted, candidate)
		}
	}
	switch len(mounted) {
	case 0:
		if len(candidates) != 0 {
			return SourceCandidate{}, fmt.Errorf("%d correlated camera storage node(s) found, but none are mounted; djpi will not mount them", len(candidates))
		}
		return SourceCandidate{}, errors.New("no mounted hardware-verified camera source found")
	case 1:
		return mounted[0], nil
	default:
		paths := make([]string, 0, len(mounted))
		for _, candidate := range mounted {
			paths = append(paths, candidate.Mountpoint)
		}
		sort.Strings(paths)
		return SourceCandidate{}, fmt.Errorf("multiple mounted camera sources are ambiguous: %s", strings.Join(paths, ", "))
	}
}

type blockInheritance struct{ serial, transport string }
type inheritedBlock struct {
	device            BlockDevice
	serial, transport string
}

func flattenBlocks(devices []BlockDevice, inherited blockInheritance) []inheritedBlock {
	result := make([]inheritedBlock, 0)
	for _, device := range devices {
		serial := device.Serial
		if serial == "" {
			serial = inherited.serial
		}
		transport := device.Transport
		if transport == "" {
			transport = inherited.transport
		}
		result = append(result, inheritedBlock{device: device, serial: serial, transport: transport})
		result = append(result, flattenBlocks(device.Children, blockInheritance{serial: serial, transport: transport})...)
	}
	return result
}

func flattenMounts(items []Filesystem) map[string]Filesystem {
	result := make(map[string]Filesystem)
	var walk func([]Filesystem)
	walk = func(entries []Filesystem) {
		for _, entry := range entries {
			result[entry.Target] = entry
			walk(entry.Children)
		}
	}
	walk(items)
	return result
}

func hasMassStorageInterface(device USBDevice) bool {
	for _, iface := range device.Interfaces {
		if strings.EqualFold(iface.Class, "08") && strings.EqualFold(iface.Subclass, "06") && strings.EqualFold(iface.Protocol, "50") {
			return true
		}
	}
	return false
}

func hasMountOption(options, wanted string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == wanted {
			return true
		}
	}
	return false
}
