// Package wireless contains the non-destructive, transport-independent pieces
// of DJI's wireless media protocol. It does not contain delete or format APIs.
package wireless

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Profile limits model-specific storage rules to hardware-verified bodies.
type Profile string

const (
	ProfileAction6 Profile = "action-6"
	ProfilePocket3 Profile = "pocket-3"
)

// MediaItem is one downloadable primary asset from a DJI CompositePack list.
// Camera file handles are deliberately not exposed: they are not cleanup proof.
type MediaItem struct {
	Path          string `json:"path"`
	ThumbnailPath string `json:"thumbnail_path"`
	Storage       int    `json:"storage"`
	SizeBytes     int64  `json:"size_bytes"`
	Kind          string `json:"kind"`
}

type mediaField struct {
	position int
	end      int
	value    string
}

type decodedRecord struct {
	item   MediaItem
	handle uint32
}

var primaryExtensions = map[string]string{
	"MP4": "video", "MOV": "video", "OSV": "video", "INSV": "video",
	"JPG": "photo", "JPEG": "photo", "DNG": "photo", "HEIC": "photo",
}

// DecodeManifest decodes an already reassembled 0x00/0x27 CompositePack
// response. It fails closed on incomplete records, duplicate paths or handles,
// and model/storage ambiguity.
func DecodeManifest(profile Profile, data []byte, streamComplete bool) ([]MediaItem, error) {
	if !streamComplete {
		return nil, errors.New("wireless manifest stream did not reach its confirmed end frame")
	}
	if profile != ProfileAction6 && profile != ProfilePocket3 {
		return nil, fmt.Errorf("unsupported wireless manifest profile %q", profile)
	}
	fields := findMediaFields(data)
	if len(fields) == 0 {
		return nil, errors.New("wireless manifest contains no CompositePack media records")
	}
	if declared := int(little32(data, 0)); declared > 0 && declared != len(fields) {
		return nil, fmt.Errorf("wireless manifest declared %d records but decoded %d", declared, len(fields))
	}
	items := make([]MediaItem, 0, len(fields))
	paths := make(map[string]struct{}, len(fields))
	handles := make(map[uint32]string, len(fields))
	for index, field := range fields {
		low := 0
		if index > 0 {
			low = fields[index-1].end
		}
		high := len(data)
		if index+1 < len(fields) {
			high = fields[index+1].position
		}
		record, err := decodeRecord(profile, data, field, low, high)
		if err != nil {
			return nil, fmt.Errorf("decode wireless record %q: %w", field.value, err)
		}
		if _, duplicate := paths[record.item.Path]; duplicate {
			return nil, fmt.Errorf("wireless manifest repeats path %q", record.item.Path)
		}
		paths[record.item.Path] = struct{}{}
		if record.handle != 0 {
			if previous, duplicate := handles[record.handle]; duplicate {
				return nil, fmt.Errorf("wireless manifest handle collision between %q and %q", previous, record.item.Path)
			}
			handles[record.handle] = record.item.Path
		}
		items = append(items, record.item)
	}
	return items, nil
}

func decodeRecord(profile Profile, data []byte, field mediaField, low, high int) (decodedRecord, error) {
	base := path.Base(field.value)
	if !safeCameraPath(field.value) || base == "." || base == "/" {
		return decodedRecord{}, errors.New("unsafe media path")
	}
	extension := ""
	thumbnail := ""
	for position := low; position < high; position++ {
		if value, ok := readPathField(data, position, 2, "MISC/"); ok && strings.HasSuffix(value.value, base) {
			thumbnail = value.value
		}
		if position+2 > len(data) || data[position] != 0x0d {
			continue
		}
		length := int(data[position+1])
		if length <= len(base) || position+2+length > len(data) {
			continue
		}
		name := string(data[position+2 : position+2+length])
		if strings.HasPrefix(name, base+".") {
			candidate := strings.ToUpper(strings.TrimPrefix(name, base+"."))
			if _, supported := primaryExtensions[candidate]; supported {
				extension = candidate
			}
		}
	}
	if extension == "" {
		return decodedRecord{}, errors.New("primary extension is missing or unsupported")
	}
	kind := primaryExtensions[extension]
	head := markerHead(data, low, field.position)
	handle := uint32(0)
	if head >= 0 {
		handle = little32(data, head)
	}
	var size uint32
	if kind == "video" && head >= 4 {
		size = little32(data, head-4)
	} else {
		marker := field.position - 7
		if field.position >= 21 && marker+1 < len(data) && data[marker] == 0x19 && data[marker+1] == 0x06 {
			size = little32(data, marker-14)
		}
		for position := low; size == 0 && position+2 < field.position; position++ {
			if (data[position] == 0xff || data[position] == 0xfe) && data[position+1] == 0x19 && data[position+2] == 0x06 && position >= 13 {
				size = little32(data, position-13)
			}
		}
	}
	if size == 0 {
		return decodedRecord{}, errors.New("manifest record has no byte size")
	}
	storage := 0
	if profile == ProfileAction6 {
		if handle == 0 {
			return decodedRecord{}, errors.New("Action 6 record has no storage-disambiguating handle")
		}
		if handle&0x40000000 != 0 {
			storage = 1
		}
	}
	if thumbnail == "" {
		thumbnail = strings.Replace(field.value, "DCIM/", "MISC/THM/", 1)
	}
	itemPath := field.value + "." + extension
	thumbnail += ".scr"
	if !safeCameraPath(itemPath) || !safeCameraPath(thumbnail) {
		return decodedRecord{}, errors.New("unsafe resolved camera path")
	}
	return decodedRecord{item: MediaItem{Path: itemPath, ThumbnailPath: thumbnail, Storage: storage, SizeBytes: int64(size), Kind: kind}, handle: handle}, nil
}

func findMediaFields(data []byte) []mediaField {
	fields := make([]mediaField, 0)
	for position := 0; position < len(data); {
		field, ok := readPathField(data, position, 1, "DCIM/")
		if !ok {
			position++
			continue
		}
		fields = append(fields, field)
		position = field.end
	}
	return fields
}

func readPathField(data []byte, position, subtype int, prefix string) (mediaField, bool) {
	if position < 0 || position+6 > len(data) || data[position] != 0x1a || data[position+2] != 0 || data[position+3] != 0 || data[position+4] != 0 || int(data[position+5]) != subtype {
		return mediaField{}, false
	}
	length := int(data[position+1]) - 6
	if length < len(prefix) || position+6+length > len(data) {
		return mediaField{}, false
	}
	value := string(data[position+6 : position+6+length])
	if !strings.HasPrefix(value, prefix) {
		return mediaField{}, false
	}
	for _, character := range []byte(value) {
		if character < 0x20 || character > 0x7e {
			return mediaField{}, false
		}
	}
	return mediaField{position: position, end: position + 6 + length, value: value}, true
}

func markerHead(data []byte, low, high int) int {
	if low < 0 {
		low = 0
	}
	if high > len(data) {
		high = len(data)
	}
	for position := low; position+3 < high; position++ {
		kind, star := data[position], data[position+1]
		if (kind == 0x03 || kind == 0x00) && (star == 0xff || star == 0xfe) && data[position+2] == 0x19 && data[position+3] == 0x06 && position >= 8 {
			return position - 8
		}
	}
	return -1
}

func little32(data []byte, position int) uint32 {
	if position < 0 || position+4 > len(data) {
		return 0
	}
	return binary.LittleEndian.Uint32(data[position : position+4])
}

func safeCameraPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

// MediaURL returns the camera-local HTTP URL for an observed item. The host is
// fixed to DJI's camera AP address to prevent configuration-driven SSRF.
func MediaURL(item MediaItem) (string, error) {
	if item.Storage != 0 && item.Storage != 1 {
		return "", errors.New("wireless storage must be 0 or 1")
	}
	if !safeCameraPath(item.Path) {
		return "", errors.New("unsafe wireless media path")
	}
	query := url.Values{"storage": {fmt.Sprint(item.Storage)}, "path": {item.Path}}
	return "http://192.168.2.1/v2?" + query.Encode(), nil
}
