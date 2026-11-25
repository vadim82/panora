package playback

import (
	"context"
	"errors"
	"testing"
)

func TestMatchesAspectRatio(t *testing.T) {
	tests := []struct {
		name  string
		image ImageInfo
		ratio AspectRatio
		want  bool
	}{
		{"ultrawide", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{32, 9}, true},
		{"larger ultrawide", ImageInfo{Width: 7680, Height: 2160}, AspectRatio{32, 9}, true},
		{"wrong ratio", ImageInfo{Width: 3840, Height: 2160}, AspectRatio{32, 9}, false},
		{"one pixel short", ImageInfo{Width: 5120, Height: 1439}, AspectRatio{32, 9}, true},
		{"one pixel tall", ImageInfo{Width: 5120, Height: 1441}, AspectRatio{32, 9}, true},
		{"two pixels short", ImageInfo{Width: 5120, Height: 1438}, AspectRatio{32, 9}, false},
		{"two pixels tall", ImageInfo{Width: 5120, Height: 1442}, AspectRatio{32, 9}, false},
		{"within one height pixel", ImageInfo{Width: 5123, Height: 1440}, AspectRatio{32, 9}, true},
		{"beyond one height pixel", ImageInfo{Width: 5124, Height: 1440}, AspectRatio{32, 9}, false},
		{"portrait one pixel wide", ImageInfo{Width: 1081, Height: 1920}, AspectRatio{9, 16}, true},
		{"portrait two pixels wide", ImageInfo{Width: 1082, Height: 1920}, AspectRatio{9, 16}, false},
		{"equivalent ratio tolerance", ImageInfo{Width: 5120, Height: 1441}, AspectRatio{64, 18}, true},
		{"empty image", ImageInfo{}, AspectRatio{32, 9}, false},
		{"negative image width", ImageInfo{Width: -5120, Height: 1440}, AspectRatio{32, 9}, false},
		{"zero image height", ImageInfo{Width: 5120}, AspectRatio{32, 9}, false},
		{"zero ratio width", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{0, 9}, false},
		{"negative ratio height", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{32, -9}, false},
		{"portrait", ImageInfo{Width: 1080, Height: 1920}, AspectRatio{9, 16}, true},
		{"equivalent ratio", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{64, 18}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.image.matchesRatio(tt.ratio, false, 1)
			if got != tt.want {
				t.Errorf("image %dx%d, ratio %d:%d: got %t, want %t",
					tt.image.Width, tt.image.Height, tt.ratio.Width, tt.ratio.Height, got, tt.want)
			}
		})
	}
}

func TestMinResolution(t *testing.T) {
	tests := []struct {
		name      string
		image     ImageInfo
		minWidth  int
		minHeight int
		want      bool
	}{
		{"exact minimum", ImageInfo{Width: 5120, Height: 1440}, 5120, 1440, true},
		{"larger image", ImageInfo{Width: 7680, Height: 2160}, 5120, 1440, true},
		{"too narrow", ImageInfo{Width: 3840, Height: 2160}, 5120, 1440, false},
		{"too short", ImageInfo{Width: 5120, Height: 1439}, 5120, 1440, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.image.meetsMinResolution(tt.minWidth, tt.minHeight)
			if got != tt.want {
				t.Errorf("image %dx%d, minimum %dx%d: got %t, want %t",
					tt.image.Width, tt.image.Height, tt.minWidth, tt.minHeight, got, tt.want)
			}
		})
	}
}

func TestMatchesAspectRatioIntegerBoundaries(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, test := range []struct {
		name  string
		image ImageInfo
		ratio AspectRatio
		want  bool
	}{
		{"huge landscape ratio", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{maxInt, 1}, false},
		{"huge portrait ratio", ImageInfo{Width: 5120, Height: 1440}, AspectRatio{1, maxInt}, false},
		{"maximum exact dimensions", ImageInfo{Width: maxInt, Height: maxInt}, AspectRatio{maxInt, maxInt}, true},
		{"maximum one pixel tolerance", ImageInfo{Width: maxInt, Height: maxInt - 1}, AspectRatio{maxInt, maxInt}, true},
		{"maximum two pixels outside tolerance", ImageInfo{Width: maxInt, Height: maxInt - 2}, AspectRatio{maxInt, maxInt}, false},
		{"large positive difference", ImageInfo{Width: maxInt, Height: 1}, AspectRatio{1, maxInt}, false},
		{"large negative difference", ImageInfo{Width: 1, Height: maxInt}, AspectRatio{maxInt, 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.image.matchesRatio(test.ratio, false, 1); got != test.want {
				t.Fatalf("%+v ratio %+v: got %v, want %v", test.image, test.ratio, got, test.want)
			}
		})
	}
	image := ImageInfo{Width: maxInt, Height: maxInt - 1}
	if image.meetsMinResolution(maxInt, maxInt) {
		t.Fatal("strict minimum height relaxed at integer boundary")
	}
}

func TestCanceledImageInspectionStopsBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readImageInfoContext(ctx, "/does-not-exist.png"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled header inspection performed file I/O: %v", err)
	}
}
