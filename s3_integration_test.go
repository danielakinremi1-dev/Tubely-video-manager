package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Integration tests against a real S3-compatible server. They're skipped
// unless TUBELY_TEST_S3_ENDPOINT is set, e.g. with the local emulator:
//
//	make up
//	TUBELY_TEST_S3_ENDPOINT=http://localhost:9090 go test -run S3 .
//
// They use the bucket from TUBELY_TEST_S3_BUCKET (default "tubely-local").
func newIntegrationConfig(t *testing.T) *apiConfig {
	t.Helper()
	endpoint := os.Getenv("TUBELY_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("TUBELY_TEST_S3_ENDPOINT not set")
	}
	bucket := os.Getenv("TUBELY_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "tubely-local"
	}
	// Emulators ignore credentials, but the SDK still signs requests.
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		t.Setenv("AWS_ACCESS_KEY_ID", "test")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	}
	client, err := newS3Client(context.Background(), "us-east-1", endpoint, true)
	if err != nil {
		t.Fatalf("newS3Client: %v", err)
	}
	return &apiConfig{s3Client: client, s3Uploader: newS3Uploader(client, true), s3Bucket: bucket}
}

func TestS3PutObjectSingleAndMultipart(t *testing.T) {
	cfg := newIntegrationConfig(t)
	ctx := context.Background()

	cases := []struct {
		name string
		size int
	}{
		{"single part", 1 << 10},
		{"multipart", 20<<20 + 123}, // over the 16 MiB threshold, uneven last part
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := bytes.Repeat([]byte("tubely!"), tc.size/7+1)[:tc.size]
			f, err := os.CreateTemp(t.TempDir(), "upload-*.mp4")
			if err != nil {
				t.Fatal(err)
			}
			f.Write(data)
			f.Seek(0, io.SeekStart)
			defer f.Close()

			key := newObjectKey("test/", ".mp4")
			if err := cfg.putObject(ctx, key, "video/mp4", f); err != nil {
				t.Fatalf("putObject: %v", err)
			}
			defer cfg.deleteObject(ctx, &key)

			out, err := cfg.s3Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &cfg.s3Bucket, Key: &key})
			if err != nil {
				t.Fatalf("GetObject: %v", err)
			}
			defer out.Body.Close()
			got, err := io.ReadAll(out.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, data) {
				t.Errorf("stored %d bytes, want %d identical bytes", len(got), len(data))
			}
			if out.ContentType == nil || *out.ContentType != "video/mp4" {
				t.Errorf("ContentType = %v, want video/mp4", out.ContentType)
			}
		})
	}
}

// A cancelled multipart upload must not leave an object or open upload behind.
func TestS3CancelledMultipartLeavesNothing(t *testing.T) {
	cfg := newIntegrationConfig(t)
	key := newObjectKey("test/", ".mp4")

	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelAfter{r: bytes.NewReader(make([]byte, 40<<20)), after: 20 << 20, cancel: cancel}
	err := cfg.putObject(ctx, key, "video/mp4", body)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("putObject err = %v, want context.Canceled", err)
	}

	bg := context.Background()
	if _, err := cfg.s3Client.HeadObject(bg, &s3.HeadObjectInput{Bucket: &cfg.s3Bucket, Key: &key}); err == nil {
		t.Errorf("object %q exists after a cancelled upload", key)
		cfg.deleteObject(bg, &key)
	}
	ups, err := cfg.s3Client.ListMultipartUploads(bg, &s3.ListMultipartUploadsInput{Bucket: &cfg.s3Bucket, Prefix: &key})
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	if len(ups.Uploads) != 0 {
		t.Errorf("%d multipart uploads left open for %q", len(ups.Uploads), key)
	}
}

// cancelAfter cancels its context once `after` bytes have been read, like a
// client disconnecting halfway through an upload.
type cancelAfter struct {
	r      io.Reader
	read   int
	after  int
	cancel context.CancelFunc
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += n
	if c.read >= c.after {
		c.cancel()
	}
	return n, err
}
