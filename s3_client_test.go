package main

import (
	"testing"

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
