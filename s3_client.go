package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
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
