package main

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// newS3Client builds an S3 client for real AWS (endpoint == "") or for any
// S3-compatible server such as a local emulator (endpoint set). Emulators
// usually need path-style addressing (host/bucket/key) because they can't
// serve virtual-hosted buckets (bucket.host/key).
//
// Over a plain-http endpoint, SigV4 has to hash the request body, so upload
// bodies must be seekable (an *os.File or bytes.Reader). Tubely always
// uploads files.
func newS3Client(ctx context.Context, region, endpoint string, pathStyle bool) (*s3.Client, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg, s3ClientOptions(endpoint, pathStyle)), nil
}

func s3ClientOptions(endpoint string, pathStyle bool) func(*s3.Options) {
	return func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = pathStyle
	}
}

// abortTimeout bounds the cleanup the transfer manager runs after a failed
// multipart upload. It gets a fresh context, so the abort still happens when
// the failure was the request being cancelled.
const abortTimeout = 30 * time.Second

// newS3Uploader wraps the client in the S3 transfer manager. Bodies above
// 16 MiB are sent as a multipart upload of 8 MiB parts in parallel, with
// per-part retries. Smaller bodies are a single PutObject. A failed multipart
// upload is aborted so S3 doesn't keep (and bill for) its uploaded parts.
//
// On real AWS every part carries a CRC32 checksum that S3 verifies. Custom
// endpoints (emulators) get checksums only when required: S3Mock doesn't
// echo per-part checksums back, so it rejects the completed upload.
func newS3Uploader(client *s3.Client, customEndpoint bool) *transfermanager.Client {
	return transfermanager.New(client, func(o *transfermanager.Options) {
		o.FailTimeout = abortTimeout
		if customEndpoint {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}
	})
}

// putObject uploads body to the media bucket under key.
func (cfg *apiConfig) putObject(ctx context.Context, key, contentType string, body io.Reader) error {
	_, err := cfg.s3Uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        body,
		ContentType: &contentType,
	})
	return err
}

// deleteObject removes an object from the media bucket. It's used for
// cleanup after the database has already changed, so failures are logged
// (leaving an orphaned object) rather than returned. A nil or empty key is a
// no-op.
//
// The context is detached from the request's cancellation so cleanup still
// runs if the client disconnects right after the response.
func (cfg *apiConfig) deleteObject(ctx context.Context, key *string) {
	if key == nil || *key == "" {
		return
	}
	_, err := cfg.s3Client.DeleteObject(context.WithoutCancel(ctx), &s3.DeleteObjectInput{
		Bucket: &cfg.s3Bucket,
		Key:    key,
	})
	if err != nil {
		log.Printf("couldn't delete object %q: %v", *key, err)
	}
}

// replaceObject deletes the previous object after a successful re-upload.
func (cfg *apiConfig) replaceObject(ctx context.Context, oldKey *string, newKey string) {
	if oldKey != nil && *oldKey != newKey {
		cfg.deleteObject(ctx, oldKey)
	}
}
