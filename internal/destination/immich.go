package destination

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.kuhree.com/kuhree/djpi/internal/source"
)

const immichAssetPrefix = "immich-asset:"

type ImmichOptions struct {
	DestinationName string
	ServerURL       string
	APIKey          string
	HTTPClient      *http.Client
}

type ImmichArchive struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewImmichArchive(options ImmichOptions) (*ImmichArchive, error) {
	if options.DestinationName == "" || options.ServerURL == "" || options.APIKey == "" {
		return nil, errors.New("Immich destination name, server URL, and API key are required")
	}
	parsed, err := url.Parse(options.ServerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Immich server URL must be an HTTP(S) API base URL without credentials, query, or fragment")
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 0}
	}
	return &ImmichArchive{name: options.DestinationName, baseURL: strings.TrimRight(options.ServerURL, "/"), apiKey: options.APIKey, client: client}, nil
}

func (d *ImmichArchive) Name() string { return d.name }

func (d *ImmichArchive) Stage(_ context.Context, _ string, namespace string, item source.ObservedItem) (StagedDelivery, error) {
	if _, err := safeNamespace(namespace); err != nil {
		return nil, err
	}
	if _, err := safeRelativePath(item.Path); err != nil {
		return nil, err
	}
	return newManagedFileStage(item.SizeBytes, func(ctx context.Context, stagedPath, digest string) (Evidence, error) {
		sha1Digest, err := hashFileSHA1(ctx, stagedPath, item.SizeBytes)
		if err != nil {
			return Evidence{}, err
		}
		evidence := Evidence{SizeBytes: item.SizeBytes, ContentDigest: digest, VerificationDigest: sha1Digest}
		assetID, upload, err := d.checkUpload(ctx, namespace+":"+digest, sha1Digest)
		if err != nil {
			return Evidence{}, err
		}
		if upload {
			assetID, err = d.uploadPath(ctx, stagedPath, item, sha1Digest)
			if err != nil {
				return Evidence{}, err
			}
		}
		evidence.Location = immichAssetPrefix + assetID
		verified, err := d.Verify(ctx, evidence)
		if err != nil {
			return Evidence{}, err
		}
		if !verified {
			return Evidence{}, fmt.Errorf("Immich verification failed for asset %s", assetID)
		}
		return evidence, nil
	})
}

func (d *ImmichArchive) Deliver(ctx context.Context, sourceRoot, namespace string, item source.ObservedItem) (Evidence, error) {
	if _, err := safeNamespace(namespace); err != nil {
		return Evidence{}, err
	}
	if _, err := safeRelativePath(item.Path); err != nil {
		return Evidence{}, err
	}
	sha256Digest, err := hashStableSource(ctx, sourceRoot, item)
	if err != nil {
		return Evidence{}, err
	}
	sha1Digest, err := hashStableSourceSHA1(ctx, sourceRoot, item)
	if err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{SizeBytes: item.SizeBytes, ContentDigest: sha256Digest, VerificationDigest: sha1Digest}

	assetID, upload, err := d.checkUpload(ctx, namespace+":"+sha256Digest, sha1Digest)
	if err != nil {
		return Evidence{}, err
	}
	if upload {
		assetID, err = d.upload(ctx, sourceRoot, item, sha1Digest)
		if err != nil {
			return Evidence{}, err
		}
	}
	evidence.Location = immichAssetPrefix + assetID
	verified, err := d.Verify(ctx, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if !verified {
		return Evidence{}, fmt.Errorf("Immich verification failed for asset %s", assetID)
	}
	stableSHA256, err := hashStableSource(ctx, sourceRoot, item)
	if err != nil {
		return Evidence{}, err
	}
	if stableSHA256 != sha256Digest {
		return Evidence{}, fmt.Errorf("source content changed while delivering %s to Immich", item.Path)
	}
	return evidence, nil
}

func (d *ImmichArchive) Verify(ctx context.Context, evidence Evidence) (bool, error) {
	assetID, ok := strings.CutPrefix(evidence.Location, immichAssetPrefix)
	if !ok || assetID == "" || strings.ContainsAny(assetID, "/?#\x00") {
		return false, fmt.Errorf("invalid Immich asset location %q", evidence.Location)
	}
	request, err := d.request(ctx, http.MethodGet, "/assets/"+url.PathEscape(assetID), nil)
	if err != nil {
		return false, err
	}
	response, err := d.client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, d.httpError(response)
	}
	var asset struct {
		ID        string `json:"id"`
		Checksum  string `json:"checksum"`
		IsTrashed bool   `json:"isTrashed"`
		IsOffline bool   `json:"isOffline"`
	}
	if err := decodeLimitedJSON(response.Body, &asset); err != nil {
		return false, fmt.Errorf("decode Immich asset: %w", err)
	}
	checksum, err := base64.StdEncoding.DecodeString(asset.Checksum)
	if err != nil {
		return false, fmt.Errorf("decode Immich asset checksum: %w", err)
	}
	wanted, ok := strings.CutPrefix(evidence.VerificationDigest, "sha1:")
	if !ok {
		return false, errors.New("Immich evidence has no SHA-1 verification digest")
	}
	return asset.ID == assetID && !asset.IsTrashed && !asset.IsOffline && hex.EncodeToString(checksum) == wanted, nil
}

func (d *ImmichArchive) checkUpload(ctx context.Context, requestID, sha1Digest string) (string, bool, error) {
	checksum := strings.TrimPrefix(sha1Digest, "sha1:")
	body := strings.NewReader(fmt.Sprintf(`{"assets":[{"id":%q,"checksum":%q}]}`, requestID, checksum))
	request, err := d.request(ctx, http.MethodPost, "/assets/bulk-upload-check", body)
	if err != nil {
		return "", false, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false, d.httpError(response)
	}
	var result struct {
		Results []struct {
			ID        string `json:"id"`
			Action    string `json:"action"`
			AssetID   string `json:"assetId"`
			Reason    string `json:"reason"`
			IsTrashed bool   `json:"isTrashed"`
		} `json:"results"`
	}
	if err := decodeLimitedJSON(response.Body, &result); err != nil {
		return "", false, fmt.Errorf("decode Immich upload check: %w", err)
	}
	if len(result.Results) != 1 || result.Results[0].ID != requestID {
		return "", false, errors.New("Immich upload check returned an unexpected result set")
	}
	checked := result.Results[0]
	switch checked.Action {
	case "accept":
		return "", true, nil
	case "reject":
		if checked.Reason != "duplicate" || checked.AssetID == "" {
			return "", false, fmt.Errorf("Immich rejected asset: %s", checked.Reason)
		}
		if checked.IsTrashed {
			return "", false, errors.New("matching Immich asset is trashed; restore it before retrying")
		}
		return checked.AssetID, false, nil
	default:
		return "", false, fmt.Errorf("Immich upload check returned unknown action %q", checked.Action)
	}
}

func (d *ImmichArchive) upload(ctx context.Context, sourceRoot string, item source.ObservedItem, sha1Digest string) (string, error) {
	return d.uploadPath(ctx, filepath.Join(sourceRoot, filepath.FromSlash(item.Path)), item, sha1Digest)
}

func (d *ImmichArchive) uploadPath(ctx context.Context, sourcePath string, item source.ObservedItem, sha1Digest string) (string, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeResult := make(chan error, 1)
	go func() {
		err := writeImmichMultipartPath(ctx, multipartWriter, sourcePath, item)
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
		writeResult <- err
	}()
	request, err := d.request(ctx, http.MethodPost, "/assets", reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		<-writeResult
		return "", err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("x-immich-checksum", strings.TrimPrefix(sha1Digest, "sha1:"))
	response, requestErr := d.client.Do(request)
	_ = reader.CloseWithError(requestErr)
	writeErr := <-writeResult
	if requestErr != nil {
		return "", requestErr
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", d.httpError(response)
	}
	var uploaded struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := decodeLimitedJSON(response.Body, &uploaded); err != nil {
		return "", fmt.Errorf("decode Immich upload: %w", err)
	}
	if uploaded.ID == "" || (uploaded.Status != "created" && uploaded.Status != "duplicate") {
		return "", errors.New("Immich upload returned incomplete evidence")
	}
	// A server may return a checksum duplicate before consuming the entire body,
	// which intentionally closes the multipart writer early. The subsequent
	// asset checksum lookup is the authoritative success condition.
	_ = writeErr
	return uploaded.ID, nil
}

func writeImmichMultipart(ctx context.Context, writer *multipart.Writer, sourceRoot string, item source.ObservedItem) error {
	return writeImmichMultipartPath(ctx, writer, filepath.Join(sourceRoot, filepath.FromSlash(item.Path)), item)
}

func writeImmichMultipartPath(ctx context.Context, writer *multipart.Writer, sourcePath string, item source.ObservedItem) error {
	modified := time.Unix(0, item.ModifiedUnixNanos).UTC().Format(time.RFC3339Nano)
	for name, value := range map[string]string{"fileCreatedAt": modified, "fileModifiedAt": modified, "filename": filepath.Base(filepath.FromSlash(item.Path)), "isFavorite": "false"} {
		if err := writer.WriteField(name, value); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile("assetData", filepath.Base(filepath.FromSlash(item.Path)))
	if err != nil {
		return err
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	written, err := copyContext(ctx, part, file)
	if err != nil {
		return err
	}
	if written != item.SizeBytes {
		return fmt.Errorf("source size changed while uploading %s", item.Path)
	}
	return nil
}

func hashFileSHA1(ctx context.Context, path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha1.New()
	written, err := copyContext(ctx, hash, file)
	if err != nil {
		return "", err
	}
	if written != size {
		return "", errors.New("staged Immich delivery size differs from Source")
	}
	return fmt.Sprintf("sha1:%x", hash.Sum(nil)), nil
}

func (d *ImmichArchive) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("x-api-key", d.apiKey)
	return request, nil
}

func hashStableSourceSHA1(ctx context.Context, root string, item source.ObservedItem) (string, error) {
	relative, err := safeRelativePath(item.Path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(filepath.Join(root, relative))
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() != item.SizeBytes || info.ModTime().UnixNano() != item.ModifiedUnixNanos {
		return "", fmt.Errorf("source item changed since inventory: %s", item.Path)
	}
	hash := sha1.New()
	written, err := copyContext(ctx, hash, file)
	if err != nil {
		return "", err
	}
	if written != item.SizeBytes {
		return "", fmt.Errorf("source size changed while hashing %s", item.Path)
	}
	return fmt.Sprintf("sha1:%x", hash.Sum(nil)), nil
}

func decodeLimitedJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 1024*1024))
	return decoder.Decode(target)
}

func (d *ImmichArchive) httpError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	message := strings.TrimSpace(string(body))
	message = strings.ReplaceAll(message, d.apiKey, "[redacted]")
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return fmt.Errorf("Immich API returned HTTP %d: %s", response.StatusCode, message)
}
