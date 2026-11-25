package main

import (
	"runtime/debug"
	"strings"
)

// Package builders set these with Go linker flags, including for source archives
// without Git metadata. An ordinary source build is identified as development.
var (
	builtVersion     string
	builtArchPackage string
	builtRevision    string
	builtDirty       string
)

type BuildIdentity struct {
	Version     string `json:"version,omitempty"`
	ArchPackage string `json:"arch_package,omitempty"`
	Revision    string `json:"revision,omitempty"`
	Development bool   `json:"development"`
}

func readBuildIdentity() BuildIdentity {
	info, _ := debug.ReadBuildInfo()
	return resolveBuildIdentity(builtVersion, builtArchPackage, builtRevision, builtDirty == "true", info)
}

func resolveBuildIdentity(version, archPackage, revision string, dirty bool, info *debug.BuildInfo) BuildIdentity {
	if revision == "" && info != nil {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				dirty = dirty || setting.Value == "true"
			}
		}
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision != "" && dirty {
		revision += " (modified)"
	}
	return BuildIdentity{
		Version: version, ArchPackage: archPackage, Revision: revision,
		Development: dirty || (version == "" && archPackage == ""),
	}
}

func (identity BuildIdentity) String() string {
	lines := []string{"Panora"}
	if identity.Development {
		lines = append(lines, "Development build")
	}
	if identity.Version != "" {
		lines = append(lines, "Version: "+identity.Version)
	}
	if identity.ArchPackage != "" {
		lines = append(lines, "Arch package: "+identity.ArchPackage)
	}
	if identity.Revision != "" {
		lines = append(lines, "Source revision: "+identity.Revision)
	}
	return strings.Join(lines, "\n")
}

// BuildIdentity reports this window's binary metadata without reading the service.
func (*Client) BuildIdentity() BuildIdentity { return readBuildIdentity() }

// TakeAboutRequest consumes the native launch intent after the page registers
// its listener. This also covers a second launch while the webview is starting.
func (c *Client) TakeAboutRequest() bool { return c.aboutRequested.Swap(false) }
