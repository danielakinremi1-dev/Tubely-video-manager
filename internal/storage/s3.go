package storage

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// S3Config selects the bucket and, optionally, an S3-compatible endpoint.
type S3Config struct {
	Bucket string
	Region string
	// Endpoint is empty for real AWS, or the URL of an S3-compatible server
	// such as a local emulator.
	Endpoint string
	// ForcePathStyle uses host/bucket/key URLs. Emulators usually need it
	// because they can't serve virtual-hosted buckets (bucket.host/key).
	ForcePathStyle bool
	// RoleARN, if set, is assumed with the base credentials (environment,
	// profile or SSO) and S3 calls use the role's temporary credentials,
	// refreshed before they expire. The AWS demo's role can only write and
	// delete objects in its bucket.
	RoleARN string
}

// S3 is a Store backed by Amazon S3 or an S3-compatible server.
//
// Over a plain-http endpoint, SigV4 has to hash the request body, so bodies
// that go up in a single request must be seekable (an *os.File or a
// multipart.File). Tubely always uploads files.
type S3 struct {
	client   *s3.Client
	uploader *transfermanager.Client
	bucket   string
}

var _ Store = (*S3)(nil)

// abortTimeout bounds the cleanup the transfer manager runs after a failed
// multipart upload. It gets a fresh context, so the abort still happens when
// the failure was the request being cancelled.
const abortTimeout = 30 * time.Second

// NewS3 builds an S3 store, loading credentials the standard AWS way
// (environment, ~/.aws, or an instance role).
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	if cfg.RoleARN != "" {
		awsCfg.Credentials = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(
			sts.NewFromConfig(awsCfg), cfg.RoleARN,
			func(o *stscreds.AssumeRoleOptions) { o.RoleSessionName = "tubely-app" },
		))
	}
	client := s3.NewFromConfig(awsCfg, s3ClientOptions(cfg.Endpoint, cfg.ForcePathStyle))
	return &S3{
		client:   client,
		uploader: newUploader(client, cfg.Endpoint != ""),
		bucket:   cfg.Bucket,
	}, nil
}

func s3ClientOptions(endpoint string, pathStyle bool) func(*s3.Options) {
	return func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = pathStyle
	}
}

// newUploader wraps the client in the S3 transfer manager. Bodies above
// 16 MiB are sent as a multipart upload of 8 MiB parts in parallel, with
// per-part retries. Smaller bodies are a single PutObject. A failed multipart
// upload is aborted so S3 doesn't keep (and bill for) its uploaded parts.
//
// On real AWS every part carries a CRC32 checksum that S3 verifies. Custom
// endpoints (emulators) get checksums only when required: S3Mock doesn't
// echo per-part checksums back, so it rejects the completed upload.
func newUploader(client *s3.Client, customEndpoint bool) *transfermanager.Client {
	return transfermanager.New(client, func(o *transfermanager.Options) {
		o.FailTimeout = abortTimeout
		if customEndpoint {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}
	})
}

func (s *S3) Put(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        body,
		ContentType: &contentType,
	})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	return err
}
