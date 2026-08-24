package probe

import (
	"sort"
	"strings"
)

// CameraModeObservation names a USB mode in which djpi recognizes the camera
// but cannot correlate a Source. It exists so an operator can tell a known
// non-storage mode apart from a detection failure. It is not camera identity:
// non-storage modes have been observed reporting placeholder USB serials, so
// nothing here may be used to identify a physical camera.
type CameraModeObservation struct {
	CameraModel string `json:"camera_model"`
	USBDevice   string `json:"usb_device"`
	USBID       string `json:"usb_id"`
	Mode        string `json:"mode"`
	Reason      string `json:"reason"`
	Guidance    string `json:"guidance"`
	Evidence    string `json:"evidence"`
}

type observedCameraMode struct {
	model    string
	mode     string
	reason   string
	evidence string
}

// These IDs come only from repository hardware evidence. Never guess IDs, and
// keep the mode and firmware stability caveats in the referenced evidence.
var observedNonStorageModes = map[string]observedCameraMode{
	"2ca3:0025": {
		model:    "DJI Osmo Action 6",
		mode:     "non-storage USB mode 2ca3:0025",
		reason:   "this mode presents no USB mass-storage interface and creates no block node",
		evidence: "docs/hardware/action-6-usb.md (observed non-storage mode)",
	},
}

const (
	switchToStorageGuidance    = "select the camera's USB storage mode to expose its media; djpi will not mount or switch modes for you"
	nonStorageSerialCaveat     = "USB serials reported in non-storage modes may be placeholders and are never used for camera identity"
	recognizedButNoStorageMode = "recognized camera USB ID without a mass-storage interface"
)

// observeCameraModes reports recognized cameras that cannot yield a Source in
// their current USB mode. Unknown USB IDs stay unlabeled raw evidence.
func observeCameraModes(usbDevices []USBDevice) []CameraModeObservation {
	observations := make([]CameraModeObservation, 0)
	for _, usb := range usbDevices {
		id := strings.ToLower(usb.VendorID + ":" + usb.ProductID)
		switch known, isNonStorage := observedNonStorageModes[id]; {
		case isNonStorage:
			observations = append(observations, CameraModeObservation{
				CameraModel: known.model, USBDevice: usb.SysName, USBID: id, Mode: known.mode,
				Reason:   known.reason + "; " + nonStorageSerialCaveat,
				Guidance: switchToStorageGuidance, Evidence: known.evidence,
			})
		default:
			identity, isCamera := observedCameraIdentities[id]
			if !isCamera || hasMassStorageInterface(usb) {
				continue
			}
			observations = append(observations, CameraModeObservation{
				CameraModel: identity.model, USBDevice: usb.SysName, USBID: id, Mode: recognizedButNoStorageMode,
				Reason:   "the camera enumerated with a known storage-mode USB ID but exposed no mass-storage interface; " + nonStorageSerialCaveat,
				Guidance: switchToStorageGuidance, Evidence: identity.evidence,
			})
		}
	}
	sort.Slice(observations, func(i, j int) bool {
		if observations[i].USBDevice != observations[j].USBDevice {
			return observations[i].USBDevice < observations[j].USBDevice
		}
		return observations[i].USBID < observations[j].USBID
	})
	return observations
}
