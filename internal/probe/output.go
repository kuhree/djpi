package probe

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

func writeTextReport(w io.Writer, report Report) error {
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}

	write("djpi read-only host probe\n")
	write("No devices are mounted, copied, deleted, or formatted by this command.\n\n")

	write("USB devices (%d)\n", len(report.USB))
	if len(report.USB) == 0 {
		write("  none visible in %s\n", usbRoot)
	}
	for _, device := range report.USB {
		write("  %s %s:%s", device.SysName, device.VendorID, device.ProductID)
		if label := strings.TrimSpace(device.Manufacturer + " " + device.Product); label != "" {
			write(" %s", label)
		}
		write("\n")
		write("    device class=%s subclass=%s protocol=%s serial=%s\n",
			showUnknown(device.Class), showUnknown(device.Subclass), showUnknown(device.Protocol), showUnknown(device.Serial))
		for _, iface := range device.Interfaces {
			write("    interface %s class=%s subclass=%s protocol=%s\n", iface.SysName,
				showUnknown(iface.Class), showUnknown(iface.Subclass), showUnknown(iface.Protocol))
		}
	}

	write("\nBlock devices (%d top-level)\n", len(report.Block))
	if len(report.Block) == 0 {
		write("  none reported\n")
	}
	writeBlocks(w, report.Block, "  ")

	write("\nMounted filesystems (%d top-level)\n", len(report.Filesystems))
	if len(report.Filesystems) == 0 {
		write("  none reported\n")
	}
	writeFilesystems(w, report.Filesystems, "  ")

	write("\nCorrelated camera Sources (%d)\n", len(report.SourceCandidates))
	if len(report.SourceCandidates) == 0 {
		write("  none found from hardware-observed identities and matching USB/block evidence\n")
	}
	for _, candidate := range report.SourceCandidates {
		write("  %s usb=%s block=%s size=%d filesystem=%s label=%s mount=%s block-read-only=%t mount-read-only=%t\n",
			candidate.CameraModel, candidate.USBDevice, candidate.BlockPath, candidate.SizeBytes,
			candidate.Filesystem, showUnknown(candidate.Label), showUnknown(candidate.Mountpoint),
			candidate.BlockReadOnly, candidate.MountReadOnly)
		write("    evidence: %s\n", candidate.IdentityEvidence)
	}

	write("\nMTP evidence\n")
	if len(report.MTP.PotentialInterfaces) == 0 {
		write("  no USB imaging/PTP interfaces visible\n")
	} else {
		for _, iface := range report.MTP.PotentialInterfaces {
			write("  potential PTP/MTP interface: %s (not proof of MTP)\n", iface)
		}
	}
	if len(report.MTP.MountedFilesystems) == 0 {
		write("  no MTP/GVFS filesystems visible\n")
	} else {
		for _, mount := range report.MTP.MountedFilesystems {
			write("  mounted evidence: %s\n", mount)
		}
	}
	tools := make([]string, 0, len(report.MTP.Tools))
	for tool := range report.MTP.Tools {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	for _, tool := range tools {
		write("  tool %s: %s\n", tool, report.MTP.Tools[tool])
	}

	if len(report.Warnings) != 0 {
		write("\nWarnings\n")
		for _, warning := range report.Warnings {
			write("  %s\n", warning)
		}
	}
	write("\nCamera identity\n")
	write("  Only repository hardware-observed USB IDs are recognized. Unknown IDs remain unlabeled raw evidence.\n")
	return nil
}

func writeBlocks(w io.Writer, devices []BlockDevice, indent string) {
	for _, device := range devices {
		fmt.Fprintf(w, "%s%s path=%s type=%s size=%d removable=%t read-only=%t transport=%s filesystem=%s mounts=%s vendor=%s model=%s serial=%s\n",
			indent, device.Name, showUnknown(device.Path), showUnknown(device.Type), device.Size, device.Removable,
			device.ReadOnly, showUnknown(device.Transport), showUnknown(device.Filesystem), showList(device.Mountpoints),
			showUnknown(device.Vendor), showUnknown(device.Model), showUnknown(device.Serial))
		writeBlocks(w, device.Children, indent+"  ")
	}
}

func writeFilesystems(w io.Writer, filesystems []Filesystem, indent string) {
	for _, item := range filesystems {
		fmt.Fprintf(w, "%s%s source=%s type=%s options=%s\n", indent, item.Target, item.Source, showUnknown(item.Type), showUnknown(item.Options))
		writeFilesystems(w, item.Children, indent+"  ")
	}
}

func showUnknown(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func showList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}
