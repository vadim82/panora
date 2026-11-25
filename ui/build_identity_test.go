package main

import (
	"runtime/debug"
	"testing"
)

func TestBuildIdentity(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef"},
		{Key: "vcs.modified", Value: "true"},
	}}
	tests := []struct {
		name, version, archPackage, revision string
		dirty                                bool
		want                                 BuildIdentity
	}{
		{
			name: "Arch release", version: "0.6.2", archPackage: "r180.29d3cf859c4b-1", revision: "29d3cf859c4b5c0b",
			want: BuildIdentity{Version: "0.6.2", ArchPackage: "r180.29d3cf859c4b-1", Revision: "29d3cf859c4b"},
		},
		{
			name: "Arch snapshot", archPackage: "r180.29d3cf859c4b-1", revision: "29d3cf859c4b5c0b",
			want: BuildIdentity{ArchPackage: "r180.29d3cf859c4b-1", Revision: "29d3cf859c4b"},
		},
		{
			name: "development checkout",
			want: BuildIdentity{Revision: "0123456789ab (modified)", Development: true},
		},
		{
			name: "macOS local bundle", version: "0.6.2", revision: "29d3cf859c4b5c0b", dirty: true,
			want: BuildIdentity{Version: "0.6.2", Revision: "29d3cf859c4b (modified)", Development: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveBuildIdentity(test.version, test.archPackage, test.revision, test.dirty, info); got != test.want {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
	if got := resolveBuildIdentity("", "", "", false, nil); got != (BuildIdentity{Development: true}) {
		t.Fatalf("source archive without package metadata: got %+v", got)
	}
}
