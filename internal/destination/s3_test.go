package destination

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"git.kuhree.com/kuhree.com/platform.djpi/internal/source"
)

type fakeS3Object struct {
	body     []byte
	metadata map[string]string
}

type fakeS3Store struct {
	objects map[string]fakeS3Object
	uploads int
}

func newFakeS3Store() *fakeS3Store {
	return &fakeS3Store{objects: make(map[string]fakeS3Object)}
}

func (f *fakeS3Store) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	object, ok := f.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, fakeS3NotFound{}
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(object.body))), Metadata: object.metadata}, nil
}

func (f *fakeS3Store) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	object, ok := f.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, fakeS3NotFound{}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(object.body)), ContentLength: aws.Int64(int64(len(object.body)))}, nil
}

func (f *fakeS3Store) Upload(_ context.Context, input *s3.PutObjectInput, _ ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	metadata := make(map[string]string, len(input.Metadata))
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	f.objects[aws.ToString(input.Key)] = fakeS3Object{body: body, metadata: metadata}
	f.uploads++
	return &manager.UploadOutput{}, nil
}

type fakeS3NotFound struct{}

func (fakeS3NotFound) Error() string                 { return "not found" }
func (fakeS3NotFound) ErrorCode() string             { return "NotFound" }
func (fakeS3NotFound) ErrorMessage() string          { return "not found" }
func (fakeS3NotFound) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestS3ArchiveDeliversVerifiesAndReusesContentAddressedObject(t *testing.T) {
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/DJI_001/clip.MP4", []byte("camera bytes"))
	store := newFakeS3Store()
	archive := &S3Archive{
		options:  S3Options{DestinationName: "backup", Region: "us-east-1", Bucket: "media", Prefix: "/djpi/"},
		head:     store,
		get:      store,
		uploader: store,
	}
	evidence, err := archive.Deliver(context.Background(), sourceRoot, "camera-one", item)
	if err != nil {
		t.Fatal(err)
	}
	wantedKey := "djpi/camera-one/DCIM/DJI_001/clip.7439bb1f96ad.MP4"
	if evidence.Location != wantedKey || evidence.ContentDigest != "sha256:7439bb1f96adccaa2164fccdcd6750e34ac61ba27ccec9a478ffa868b9c347cb" {
		t.Fatalf("evidence = %#v", evidence)
	}
	if got := store.objects[wantedKey].body; !bytes.Equal(got, []byte("camera bytes")) {
		t.Fatalf("stored body = %q", got)
	}
	verified, err := archive.Verify(context.Background(), evidence)
	if err != nil || !verified {
		t.Fatalf("verify = %t, %v", verified, err)
	}
	second, err := archive.Deliver(context.Background(), sourceRoot, "camera-one", item)
	if err != nil || second != evidence || store.uploads != 1 {
		t.Fatalf("idempotent delivery = %#v, uploads=%d, err=%v", second, store.uploads, err)
	}
}

func TestS3ArchiveCommitsFannedOutBytesAndReadsThemBack(t *testing.T) {
	content := []byte("camera bytes")
	item := source.ObservedItem{Path: "DCIM/clip.MP4", SizeBytes: int64(len(content))}
	store := newFakeS3Store()
	archive := &S3Archive{options: S3Options{DestinationName: "objects", Region: "r", Bucket: "media"}, head: store, get: store, uploader: store}
	stage, err := archive.Stage(context.Background(), "/camera", "action6", item)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Abort()
	if _, err := stage.Write(context.Background(), content); err != nil {
		t.Fatal(err)
	}
	evidence, err := stage.Commit(context.Background(), "sha256:7439bb1f96adccaa2164fccdcd6750e34ac61ba27ccec9a478ffa868b9c347cb")
	if err != nil {
		t.Fatal(err)
	}
	if store.uploads != 1 || !bytes.Equal(store.objects[evidence.Location].body, content) {
		t.Fatalf("uploads=%d evidence=%#v object=%#v", store.uploads, evidence, store.objects[evidence.Location])
	}
}

func TestS3ArchiveRefusesExistingObjectWithMismatchedEvidence(t *testing.T) {
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	store := newFakeS3Store()
	archive := &S3Archive{options: S3Options{DestinationName: "backup", Region: "r", Bucket: "b"}, head: store, get: store, uploader: store}
	digest, err := hashStableSource(context.Background(), sourceRoot, item)
	if err != nil {
		t.Fatal(err)
	}
	key := contentLocation("camera", digest, item.Path)
	store.objects[key] = fakeS3Object{body: []byte("other"), metadata: map[string]string{"djpi-sha256": "wrong", "djpi-size": fmt.Sprint(item.SizeBytes)}}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil {
		t.Fatal("expected existing-object conflict")
	}
	if store.uploads != 0 {
		t.Fatalf("unexpected uploads: %d", store.uploads)
	}
}

func TestS3ArchiveRejectsCorrectMetadataOnWrongRemoteBytes(t *testing.T) {
	sourceRoot := t.TempDir()
	item := fixtureItem(t, sourceRoot, "DCIM/photo.JPG", []byte("photo"))
	store := newFakeS3Store()
	archive := &S3Archive{options: S3Options{DestinationName: "backup", Region: "r", Bucket: "b"}, head: store, get: store, uploader: store}
	digest, err := hashStableSource(context.Background(), sourceRoot, item)
	if err != nil {
		t.Fatal(err)
	}
	key := contentLocation("camera", digest, item.Path)
	store.objects[key] = fakeS3Object{
		body:     []byte("wrong"),
		metadata: map[string]string{"djpi-sha256": strings.TrimPrefix(digest, "sha256:"), "djpi-size": fmt.Sprint(item.SizeBytes)},
	}
	evidence := Evidence{SizeBytes: item.SizeBytes, ContentDigest: digest, VerificationDigest: digest, Location: key}
	verified, err := archive.Verify(context.Background(), evidence)
	if err != nil || verified {
		t.Fatalf("verify = %t, %v", verified, err)
	}
	if _, err := archive.Deliver(context.Background(), sourceRoot, "camera", item); err == nil {
		t.Fatal("expected byte-level existing-object conflict")
	}
	if store.uploads != 0 {
		t.Fatalf("unexpected overwrite: %d uploads", store.uploads)
	}
}

func TestS3ArchiveRejectsUnsafeSourcePathAndNamespace(t *testing.T) {
	store := newFakeS3Store()
	archive := &S3Archive{options: S3Options{DestinationName: "backup", Region: "r", Bucket: "b"}, head: store, get: store, uploader: store}
	if _, err := archive.Deliver(context.Background(), t.TempDir(), "camera", fixtureUnsafeItem("../escape")); err == nil {
		t.Fatal("expected unsafe path refusal")
	}
	if _, err := archive.Deliver(context.Background(), t.TempDir(), "../camera", fixtureUnsafeItem("clip.mp4")); err == nil {
		t.Fatal("expected unsafe namespace refusal")
	}
}

func TestS3OptionsRejectCredentialBearingEndpoint(t *testing.T) {
	err := validateS3Options(S3Options{DestinationName: "backup", Region: "r", Bucket: "b", Endpoint: "https://user:secret@objects.example"})
	if err == nil {
		t.Fatal("expected unsafe endpoint refusal")
	}
}

func TestS3UploadConcurrencyDefaultsAndBounds(t *testing.T) {
	options := S3Options{DestinationName: "backup", Region: "r", Bucket: "b"}
	if got := s3UploadConcurrency(options); got != 4 {
		t.Fatalf("default concurrency = %d", got)
	}
	options.UploadConcurrency = 12
	if got := s3UploadConcurrency(options); got != 12 {
		t.Fatalf("configured concurrency = %d", got)
	}
	options.UploadConcurrency = 33
	if err := validateS3Options(options); err == nil {
		t.Fatal("expected excessive concurrency refusal")
	}
}

func fixtureUnsafeItem(path string) source.ObservedItem {
	return source.ObservedItem{Path: path, SizeBytes: 1}
}
