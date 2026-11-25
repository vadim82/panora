package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

// This is a bounded read-only projection for thin shell clients. It deliberately
// excludes filesystem paths, raw provider diagnostics and private configuration.
type shellSnapshot struct {
	Version       int         `json:"version"`
	Connected     bool        `json:"connected"`
	Code          string      `json:"code,omitempty"`
	InstanceID    string      `json:"instance_id,omitempty"`
	Paused        bool        `json:"paused"`
	Busy          bool        `json:"busy"`
	Uncertain     bool        `json:"uncertain"`
	SetupRequired bool        `json:"setup_required"`
	CanPrevious   bool        `json:"can_previous"`
	Warning       bool        `json:"warning"`
	NextAt        *time.Time  `json:"next_at,omitempty"`
	Image         *shellImage `json:"image,omitempty"`
}

type shellImage struct {
	ID           string `json:"id"`
	HistoryID    string `json:"history_id"`
	Title        string `json:"title"`
	Origin       string `json:"origin"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	PreviewState string `json:"preview_state"`
	DataURI      string `json:"data_uri,omitempty"`
}

func newShellCommand() *cobra.Command {
	group := &cobra.Command{Use: "shell", Short: "Read bounded data for a thin desktop shell client"}
	var socket string
	var preview bool
	command := &cobra.Command{
		Use: "snapshot", Short: "Print a versioned read-only shell snapshot as JSON", Args: cobra.NoArgs,
		Long: "Read playback status without starting a service or changing the desktop.\n--preview includes current-image details from status and requests a bounded cached PNG.\nA disconnected service is represented by connected:false, without raw service diagnostics.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := control.SocketPath(socket)
			result := shellSnapshot{Version: 1, Code: "service_unavailable"}
			if err == nil {
				ctx, cancel := context.WithTimeout(cmd.Context(), 6*time.Second)
				defer cancel()
				result = readShellSnapshot(ctx, path, preview)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	command.Flags().StringVar(&socket, "socket", "", "private service control socket")
	command.Flags().BoolVar(&preview, "preview", false, "include current-image details and a validated cached PNG when available")
	group.AddCommand(command)
	return group
}

func readShellSnapshot(ctx context.Context, path string, preview bool) shellSnapshot {
	status, err := control.Request(ctx, path, "")
	if err != nil {
		return shellSnapshot{Version: 1, Code: "service_unavailable"}
	}
	result := projectShellStatus(status)
	if !result.Connected || !preview || result.Image == nil {
		return result
	}
	result.Image.PreviewState = "unavailable"
	if entry := status.CurrentImage; entry != nil {
		switch entry.Provider {
		case "local":
			result.Image.Origin = "Local folders"
			if entry.OriginalPath != "" {
				result.Image.Title = shellText(filepath.Base(entry.OriginalPath), 256)
			}
		case "omarchy":
			result.Image.Origin = "Omarchy theme"
			if entry.OriginalPath != "" {
				result.Image.Title = shellText(filepath.Base(entry.OriginalPath), 256)
			}
		case "wallhaven":
			result.Image.Origin = "Wallhaven"
			result.Image.Title = "Wallhaven " + shellText(entry.Image, 128)
		}
		if entry.Dimensions != nil && entry.Dimensions.Width > 0 && entry.Dimensions.Height > 0 {
			result.Image.Width, result.Image.Height = entry.Dimensions.Width, entry.Dimensions.Height
		}
	}
	thumbnail, err := control.ReadPreview(ctx, path, status.Current.ImageID)
	if err == nil && thumbnail.Available {
		if validShellPreview(thumbnail, status.Current.ImageID) {
			result.Image.PreviewState = "available"
			result.Image.DataURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(thumbnail.Data)
		} else {
			result.Image.PreviewState = "invalid"
		}
	}
	// The service/image may have changed while the preview was read.
	// Discard all old detail data; the client can request the new identity once.
	latest, err := control.Request(ctx, path, "")
	if err != nil {
		return shellSnapshot{Version: 1, Code: "service_unavailable"}
	}
	if latest.InstanceID != status.InstanceID || latest.Current == nil ||
		latest.Current.ID != status.Current.ID || latest.Current.ImageID != status.Current.ImageID {
		return projectShellStatus(latest)
	}
	// Apply the newest pause/busy/warning state while retaining matched details.
	fresh := projectShellStatus(latest)
	if fresh.Connected {
		fresh.Image = result.Image
	}
	return fresh
}

func projectShellStatus(status playback.Status) shellSnapshot {
	if status.Version != 1 || status.InstanceID == "" || len(status.InstanceID) > 128 ||
		status.HistoryLength < 0 || status.HistoryLength > playback.HistoryLimit ||
		status.HistoryCursor < -1 || status.HistoryCursor >= max(status.HistoryLength, 1) {
		return shellSnapshot{Version: 1, Code: "invalid_status"}
	}
	result := shellSnapshot{Version: 1, Connected: true, InstanceID: status.InstanceID,
		Paused: status.Paused, Busy: status.Busy, Uncertain: status.Uncertain,
		SetupRequired: status.SetupRequired, CanPrevious: status.HistoryCursor > 0,
		Warning: len(status.Warnings) > 0 || len(status.RefreshWarnings) > 0 || status.LastError != nil,
		NextAt:  status.NextAt}
	if status.Current != nil {
		if status.Current.ImageID <= 0 || status.Current.ID == "" || len(status.Current.ID) > 64 {
			return shellSnapshot{Version: 1, Code: "invalid_status"}
		}
		result.Image = &shellImage{ID: strconv.FormatInt(status.Current.ImageID, 10),
			HistoryID: status.Current.ID, Title: "Accepted image", Origin: "Configured source", PreviewState: "not_requested"}
	}
	return result
}

func validShellPreview(preview playback.ImagePreview, id int64) bool {
	if preview.Version != 1 || preview.ImageID != id || preview.MIMEType != "image/png" ||
		len(preview.Data) == 0 || len(preview.Data) > playback.MaxPreviewBytes {
		return false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(preview.Data))
	if err != nil || format != "png" || config.Width != preview.Width || config.Height != preview.Height ||
		config.Width < 1 || config.Height < 1 || config.Width > playback.MaxPreviewDimension || config.Height > playback.MaxPreviewDimension {
		return false
	}
	_, err = png.Decode(bytes.NewReader(preview.Data))
	return err == nil
}

func shellText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, value)
	return string([]rune(value)[:min(len([]rune(value)), limit)])
}
