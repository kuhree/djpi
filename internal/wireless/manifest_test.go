package wireless

import (
	"encoding/base64"
	"os"
	"sort"
	"strings"
	"testing"
)

func TestDecodeAction6HardwareManifests(t *testing.T) {
	sd, err := DecodeManifest(ProfileAction6, fixture(t, "oa6_sd_3.bin.b64"), true)
	if err != nil {
		t.Fatal(err)
	}
	internal, err := DecodeManifest(ProfileAction6, fixture(t, "oa6_internal_2.bin.b64"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(sd) != 3 || len(internal) != 2 {
		t.Fatalf("record counts: SD=%d internal=%d", len(sd), len(internal))
	}
	if !allStorage(sd, 0) || !allStorage(internal, 1) {
		t.Fatalf("storage resolution: SD=%#v internal=%#v", sd, internal)
	}
	sdSizes := itemSizes(sd)
	internalSizes := itemSizes(internal)
	if !equalInt64s(sdSizes, []int64{2_408_448, 42_051_246, 127_391_571}) {
		t.Fatalf("SD sizes = %v", sdSizes)
	}
	if !equalInt64s(internalSizes, []int64{2_834_432, 39_513_171}) {
		t.Fatalf("internal sizes = %v", internalSizes)
	}
}

func TestDecodePocket3HardwareManifest(t *testing.T) {
	items, err := DecodeManifest(ProfilePocket3, fixture(t, "op3_11_panos.bin.b64"), true)
	if err != nil {
		t.Fatal(err)
	}
	photos, videos := 0, 0
	for _, item := range items {
		if item.SizeBytes <= 0 || item.Storage != 0 {
			t.Fatalf("incomplete Pocket 3 item: %#v", item)
		}
		switch item.Kind {
		case "photo":
			photos++
		case "video":
			videos++
		}
	}
	if len(items) != 11 || photos != 8 || videos != 3 {
		t.Fatalf("items=%d photos=%d videos=%d", len(items), photos, videos)
	}
}

func TestDecodeManifestFailsClosedOnIncompleteStreamAndHandleCollision(t *testing.T) {
	fixtureBytes := fixture(t, "oa6_sd_3.bin.b64")
	if _, err := DecodeManifest(ProfileAction6, fixtureBytes, false); err == nil {
		t.Fatal("expected incomplete stream refusal")
	}
	fields := findMediaFields(fixtureBytes)
	if len(fields) < 2 {
		t.Fatal("fixture has fewer than two records")
	}
	firstHead := markerHead(fixtureBytes, 0, fields[0].position)
	secondHead := markerHead(fixtureBytes, fields[0].end, fields[1].position)
	corrupt := append([]byte(nil), fixtureBytes...)
	copy(corrupt[secondHead:secondHead+4], corrupt[firstHead:firstHead+4])
	if _, err := DecodeManifest(ProfileAction6, corrupt, true); err == nil || !strings.Contains(err.Error(), "handle collision") {
		t.Fatalf("error = %v", err)
	}
}

func TestMediaURLIsFixedToCameraAndEscapesPath(t *testing.T) {
	got, err := MediaURL(MediaItem{Path: "DCIM/DJI_001/clip name.MP4", Storage: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://192.168.2.1/v2?path=DCIM%2FDJI_001%2Fclip+name.MP4&storage=1" {
		t.Fatalf("URL = %q", got)
	}
	if _, err := MediaURL(MediaItem{Path: "../escape", Storage: 0}); err == nil {
		t.Fatal("expected unsafe path refusal")
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func allStorage(items []MediaItem, wanted int) bool {
	for _, item := range items {
		if item.Storage != wanted {
			return false
		}
	}
	return true
}

func itemSizes(items []MediaItem) []int64 {
	result := make([]int64, 0, len(items))
	for _, item := range items {
		result = append(result, item.SizeBytes)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func equalInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
