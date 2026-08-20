package destination

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"git.kuhree.com/kuhree.com/djpi/internal/source"
)

type S3Options struct {
	DestinationName string
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	UsePathStyle    bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type s3HeadClient interface {
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

type s3UploadClient interface {
	Upload(context.Context, *s3.PutObjectInput, ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}

type S3Archive struct {
	options  S3Options
	head     s3HeadClient
	uploader s3UploadClient
}

func NewS3Archive(ctx context.Context, options S3Options) (*S3Archive, error) {
	if err := validateS3Options(options); err != nil {
		return nil, err
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(options.Region)}
	if options.AccessKeyID != "" || options.SecretAccessKey != "" {
		if options.AccessKeyID == "" || options.SecretAccessKey == "" {
			return nil, errors.New("both S3 access key and secret key are required")
		}
		provider := credentials.NewStaticCredentialsProvider(options.AccessKeyID, options.SecretAccessKey, options.SessionToken)
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(provider))
	}
	configuration, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(configuration, func(value *s3.Options) {
		value.UsePathStyle = options.UsePathStyle
		if options.Endpoint != "" {
			value.BaseEndpoint = aws.String(options.Endpoint)
		}
	})
	uploader := manager.NewUploader(client, func(value *manager.Uploader) {
		value.PartSize = 16 * 1024 * 1024
		value.Concurrency = 2
		value.LeavePartsOnError = false
	})
	return &S3Archive{options: options, head: client, uploader: uploader}, nil
}

func (d *S3Archive) Name() string { return d.options.DestinationName }

func (d *S3Archive) Deliver(ctx context.Context, sourceRoot, namespace string, item source.ObservedItem) (Evidence, error) {
	if err := validateS3Options(d.options); err != nil {
		return Evidence{}, err
	}
	if _, err := safeNamespace(namespace); err != nil {
		return Evidence{}, err
	}
	if _, err := safeRelativePath(item.Path); err != nil {
		return Evidence{}, err
	}
	digest, err := hashStableSource(ctx, sourceRoot, item)
	if err != nil {
		return Evidence{}, err
	}
	evidence := Evidence{SizeBytes: item.SizeBytes, ContentDigest: digest, VerificationDigest: digest}
	evidence.Location = s3ObjectKey(d.options.Prefix, contentLocation(namespace, digest, item.Path))

	exists, matches, err := d.headMatches(ctx, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if exists {
		if !matches {
			return Evidence{}, fmt.Errorf("S3 destination conflict at s3://%s/%s", d.options.Bucket, evidence.Location)
		}
		stable, err := hashStableSource(ctx, sourceRoot, item)
		if err != nil {
			return Evidence{}, err
		}
		if stable != digest {
			return Evidence{}, fmt.Errorf("source content changed while verifying existing object %s", item.Path)
		}
		return evidence, nil
	}

	sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(item.Path))
	file, err := os.Open(sourcePath)
	if err != nil {
		return Evidence{}, err
	}
	_, uploadErr := d.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(d.options.Bucket), Key: aws.String(evidence.Location), Body: file,
		ContentLength: aws.Int64(item.SizeBytes), ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
		Metadata: map[string]string{"djpi-sha256": strings.TrimPrefix(digest, "sha256:"), "djpi-size": strconv.FormatInt(item.SizeBytes, 10)},
	})
	closeErr := file.Close()
	if uploadErr != nil {
		return Evidence{}, uploadErr
	}
	if closeErr != nil {
		return Evidence{}, closeErr
	}
	stable, err := hashStableSource(ctx, sourceRoot, item)
	if err != nil {
		return Evidence{}, err
	}
	if stable != digest {
		return Evidence{}, fmt.Errorf("source content changed while uploading %s", item.Path)
	}
	_, matches, err = d.headMatches(ctx, evidence)
	if err != nil {
		return Evidence{}, err
	}
	if !matches {
		return Evidence{}, fmt.Errorf("S3 verification failed after upload: s3://%s/%s", d.options.Bucket, evidence.Location)
	}
	return evidence, nil
}

func (d *S3Archive) Verify(ctx context.Context, evidence Evidence) (bool, error) {
	_, matches, err := d.headMatches(ctx, evidence)
	return matches, err
}

func (d *S3Archive) headMatches(ctx context.Context, evidence Evidence) (bool, bool, error) {
	output, err := d.head.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(d.options.Bucket), Key: aws.String(evidence.Location)})
	if err != nil {
		if isS3NotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if output.ContentLength == nil || *output.ContentLength != evidence.SizeBytes {
		return true, false, nil
	}
	digest := metadataValue(output.Metadata, "djpi-sha256")
	size := metadataValue(output.Metadata, "djpi-size")
	return true, digest == strings.TrimPrefix(evidence.ContentDigest, "sha256:") && size == strconv.FormatInt(evidence.SizeBytes, 10), nil
}

func validateS3Options(options S3Options) error {
	if options.DestinationName == "" || options.Region == "" || options.Bucket == "" {
		return errors.New("S3 destination name, region, and bucket are required")
	}
	if options.Endpoint != "" {
		parsed, err := url.Parse(options.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("S3 endpoint must be an HTTP(S) URL without credentials, query, or fragment")
		}
	}
	return nil
}

func s3ObjectKey(prefix, location string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return location
	}
	return prefix + "/" + location
}

func metadataValue(metadata map[string]string, wanted string) string {
	for key, value := range metadata {
		if strings.EqualFold(key, wanted) {
			return value
		}
	}
	return ""
}

func isS3NotFound(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) && (apiError.ErrorCode() == "NotFound" || apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NoSuchObject") {
		return true
	}
	var statusError interface{ HTTPStatusCode() int }
	return errors.As(err, &statusError) && statusError.HTTPStatusCode() == 404
}

func hashStableSource(ctx context.Context, root string, item source.ObservedItem) (string, error) {
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
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			written, writeErr := hash.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return "", writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	if total != item.SizeBytes {
		return "", fmt.Errorf("source size changed while hashing %s", item.Path)
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}
