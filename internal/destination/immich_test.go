package destination

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestImmichArchiveUploadsVerifiesAndReusesDuplicate(t *testing.T) {
	const assetID = "11111111-1111-4111-8111-111111111111"
	content := []byte("camera bytes")
	sha1Sum := sha1.Sum(content)
	sha1Hex := hex.EncodeToString(sha1Sum[:])
	var mu sync.Mutex
	bulkChecks, uploads, lookups := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-api-key") != "test-key" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/assets/bulk-upload-check":
			var body struct {
				Assets []struct {
					ID       string `json:"id"`
					Checksum string `json:"checksum"`
				} `json:"assets"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Assets) != 1 || body.Assets[0].Checksum != sha1Hex {
				t.Errorf("bulk request = %#v, err=%v", body, err)
			}
			mu.Lock()
			bulkChecks++
			current := bulkChecks
			mu.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			if current == 1 {
				_, _ = writer.Write([]byte(`{"results":[{"id":` + jsonString(body.Assets[0].ID) + `,"action":"accept"}]}`))
			} else {
				_, _ = writer.Write([]byte(`{"results":[{"id":` + jsonString(body.Assets[0].ID) + `,"action":"reject","reason":"duplicate","assetId":"` + assetID + `","isTrashed":false}]}`))
			}
		case request.Method == http.MethodPost && request.URL.Path == "/api/assets":
			if request.Header.Get("x-immich-checksum") != sha1Hex {
				t.Errorf("checksum header = %q", request.Header.Get("x-immich-checksum"))
			}
			if err := request.ParseMultipartForm(1024 * 1024); err != nil {
				t.Errorf("parse multipart: %v", err)
				http.Error(writer, "bad multipart", http.StatusBadRequest)
				return
			}
			file, _, err := request.FormFile("assetData")
			if err != nil {
				t.Errorf("assetData: %v", err)
				return
			}
			got, _ := io.ReadAll(file)
			_ = file.Close()
			if string(got) != string(content) || request.FormValue("filename") != "clip.MP4" || request.FormValue("fileCreatedAt") == "" {
				t.Errorf("multipart content=%q fields=%v", got, request.MultipartForm.Value)
			}
			mu.Lock()
			uploads++
			mu.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"id":"` + assetID + `","status":"created"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/assets/"+assetID:
			mu.Lock()
			lookups++
			mu.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"` + assetID + `","checksum":"` + base64.StdEncoding.EncodeToString(sha1Sum[:]) + `","isTrashed":false,"isOffline":false}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/clip.MP4", content)
	archive, err := NewImmichArchive(ImmichOptions{DestinationName: "photos", ServerURL: server.URL + "/api", APIKey: "test-key", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := archive.Deliver(context.Background(), sourceRoot, "camera", item)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Location != immichAssetPrefix+assetID || evidence.VerificationDigest != "sha1:"+sha1Hex || !strings.HasPrefix(evidence.ContentDigest, "sha256:") {
		t.Fatalf("evidence = %#v", evidence)
	}
	second, err := archive.Deliver(context.Background(), sourceRoot, "camera", item)
	if err != nil || second != evidence {
		t.Fatalf("duplicate delivery = %#v, %v", second, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if bulkChecks != 2 || uploads != 1 || lookups != 2 {
		t.Fatalf("requests: bulk=%d uploads=%d lookups=%d", bulkChecks, uploads, lookups)
	}
}

func TestImmichArchiveRefusesTrashedDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/assets/bulk-upload-check" {
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
		var body struct {
			Assets []struct {
				ID string `json:"id"`
			} `json:"assets"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		_, _ = writer.Write([]byte(`{"results":[{"id":` + jsonString(body.Assets[0].ID) + `,"action":"reject","reason":"duplicate","assetId":"11111111-1111-4111-8111-111111111111","isTrashed":true}]}`))
	}))
	defer server.Close()
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "photo.JPG", []byte("photo"))
	archive, err := NewImmichArchive(ImmichOptions{DestinationName: "photos", ServerURL: server.URL + "/api", APIKey: "key", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil || !strings.Contains(err.Error(), "trashed") {
		t.Fatalf("error = %v", err)
	}
}

func TestImmichArchiveVerificationRequiresStoredChecksumAndLiveAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"id":"11111111-1111-4111-8111-111111111111","checksum":"AAAAAAAAAAAAAAAAAAAAAAAAAAA=","isTrashed":false,"isOffline":false}`))
	}))
	defer server.Close()
	archive, err := NewImmichArchive(ImmichOptions{DestinationName: "photos", ServerURL: server.URL, APIKey: "key", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := archive.Verify(context.Background(), Evidence{Location: immichAssetPrefix + "11111111-1111-4111-8111-111111111111", VerificationDigest: "sha1:ffffffffffffffffffffffffffffffffffffffff"})
	if err != nil || verified {
		t.Fatalf("verify = %t, %v", verified, err)
	}
}

func TestNewImmichArchiveRejectsUnsafeOrIncompleteOptions(t *testing.T) {
	for _, options := range []ImmichOptions{
		{},
		{DestinationName: "photos", ServerURL: "ftp://photos/api", APIKey: "key"},
		{DestinationName: "photos", ServerURL: "https://user:pass@photos/api", APIKey: "key"},
		{DestinationName: "photos", ServerURL: "https://photos/api?key=secret", APIKey: "key"},
	} {
		if _, err := NewImmichArchive(options); err == nil {
			t.Fatalf("expected invalid options: %#v", options)
		}
	}
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
