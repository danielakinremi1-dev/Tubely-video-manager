package main

import "testing"

func TestResolveMediaBaseURL(t *testing.T) {
	tests := []struct {
		name, media, cf, want string
		wantErr               bool
	}{
		{"media base wins", "http://localhost:9090/tubely-local", "dxxxx.cloudfront.net", "http://localhost:9090/tubely-local", false},
		{"trailing slash trimmed", "https://cdn.example.com/", "", "https://cdn.example.com", false},
		{"legacy cloudfront fallback", "", "dxxxx.cloudfront.net", "https://dxxxx.cloudfront.net", false},
		{"neither set", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMediaBaseURL(tt.media, tt.cf)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
