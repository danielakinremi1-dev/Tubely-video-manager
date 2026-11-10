package storage

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3ClientOptions(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		pathStyle    bool
		wantEndpoint string
	}{
		{"real AWS", "", false, ""},
		{"emulator", "http://localhost:9090", true, "http://localhost:9090"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var o s3.Options
			s3ClientOptions(tt.endpoint, tt.pathStyle)(&o)

			got := ""
			if o.BaseEndpoint != nil {
				got = *o.BaseEndpoint
			}
			if got != tt.wantEndpoint {
				t.Errorf("BaseEndpoint = %q, want %q", got, tt.wantEndpoint)
			}
			if o.UsePathStyle != tt.pathStyle {
				t.Errorf("UsePathStyle = %v, want %v", o.UsePathStyle, tt.pathStyle)
			}
		})
	}
}

// With RoleARN set, S3 calls use the assumed role's credentials. Building the
// store makes no AWS calls; the role is assumed on first use.
func TestNewS3AssumesRole(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "base")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "base")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/none")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/none")
	// A profile exported in the developer's shell (AWS_PROFILE=sandbox) would
	// point at a profile the empty config above doesn't have. Setenv first so
	// the original value is restored after the test.
	for _, v := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		t.Setenv(v, "")
		if err := os.Unsetenv(v); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		role string
		want bool
	}{
		{"", false},
		{"arn:aws:iam::123456789012:role/tubely-demo-app", true},
	} {
		s, err := NewS3(context.Background(), S3Config{Bucket: "b", Region: "us-east-1", RoleARN: tc.role})
		if err != nil {
			t.Fatal(err)
		}
		got := aws.IsCredentialsProvider(s.client.Options().Credentials, (*stscreds.AssumeRoleProvider)(nil))
		if got != tc.want {
			t.Errorf("RoleARN %q: assume-role provider = %v, want %v", tc.role, got, tc.want)
		}
	}
}
