package playback

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseAspectRatio(t *testing.T) {
	tests := []struct {
		input string
		want  AspectRatio
	}{
		{"32:9", AspectRatio{32, 9}},
		{"16:9", AspectRatio{16, 9}},
		{"9:16", AspectRatio{9, 16}},
		{"1:1", AspectRatio{1, 1}},
		{"43:18", AspectRatio{43, 18}},
		{"64:18", AspectRatio{64, 18}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAspectRatio(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseAspectRatioInvalid(t *testing.T) {
	tests := []struct {
		input  string
		reason string
	}{
		{"", "expected width:height"},
		{"32", "expected width:height"},
		{"32x9", "expected width:height"},
		{"wide:9", "width must be an integer"},
		{":9", "width must be an integer"},
		{"32:", "height must be an integer"},
		{"32:tall", "height must be an integer"},
		{"32:9:1", "height must be an integer"},
		{"3.2:9", "width must be an integer"},
		{"999999999999999999999999:9", "width must be an integer"},
		{"32:0", "must be positive"},
		{"0:9", "must be positive"},
		{"-32:9", "must be positive"},
		{"32:-9", "must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAspectRatio(tt.input)
			if err == nil {
				t.Fatalf("expected error, got %+v", got)
			}
			if !strings.Contains(err.Error(), tt.reason) {
				t.Errorf("error %q should contain %q", err, tt.reason)
			}
			if got != (AspectRatio{}) {
				t.Errorf("expected zero value on error, got %+v", got)
			}
		})
	}
}

func TestParseAspectRatioPlatformIntegerBoundary(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	text := strconv.Itoa(maxInt)
	ratio, err := ParseAspectRatio(text + ":1")
	if err != nil || ratio.Width != maxInt || ratio.Height != 1 {
		t.Fatalf("positive platform int rejected: %+v %v", ratio, err)
	}
	if _, err := ParseAspectRatio(text + "0:1"); err == nil {
		t.Fatal("ratio outside platform int accepted")
	}
}
