package media

import "testing"

func TestClassifyAspectRatio(t *testing.T) {
	tests := []struct {
		w, h int
		want string
	}{
		{1920, 1080, Landscape},
		{1920, 1088, Landscape}, // encoder padding to a multiple of 16
		{1720, 1000, Landscape}, // 1.72, lower edge
		{1719, 1000, Other},
		{1840, 1000, Landscape}, // 1.84, upper edge
		{1841, 1000, Other},
		{1080, 1920, Portrait},
		{500, 1000, Portrait}, // 0.50, lower edge
		{499, 1000, Other},
		{620, 1000, Portrait}, // 0.62, upper edge
		{621, 1000, Other},
		{640, 480, Other}, // 4:3
		{1000, 1000, Other},
		{0, 1080, Other},
		{1920, 0, Other}, // no division by zero
		{-1920, 1080, Other},
	}
	for _, tt := range tests {
		if got := ClassifyAspectRatio(tt.w, tt.h); got != tt.want {
			t.Errorf("ClassifyAspectRatio(%d, %d) = %q, want %q", tt.w, tt.h, got, tt.want)
		}
	}
	if got := (Info{Width: 1080, Height: 1920}).AspectRatio(); got != Portrait {
		t.Errorf("Info.AspectRatio = %q, want %q", got, Portrait)
	}
}
