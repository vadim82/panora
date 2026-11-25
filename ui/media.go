package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/cache"
	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/playback"
)

type PreviewResult struct {
	Preview *playback.ImagePreview `json:"preview,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Code    string                 `json:"code,omitempty"`
}

// Preview accepts an identity, never a filesystem path or remote URL. The service
// owns cache validation and bounded thumbnail creation; the client never reads originals.
func (c *Client) Preview(imageID int64) PreviewResult {
	return c.preview(imageID, "")
}

func (c *Client) PreviewNeighbor(imageID int64, historyID string) PreviewResult {
	return c.preview(imageID, historyID)
}

func (c *Client) preview(imageID int64, historyID string) PreviewResult {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	var preview playback.ImagePreview
	var err error
	if historyID == "" {
		preview, err = control.ReadPreview(ctx, c.socket, imageID)
	} else {
		preview, err = control.ReadNeighborPreview(ctx, c.socket, imageID, historyID)
	}
	if err != nil {
		return PreviewResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	if preview.ImageID != imageID {
		return PreviewResult{Error: "The preview identity changed. Refresh status to try again.", Code: "preview_invalid"}
	}
	if preview.Available {
		config, format, decodeErr := image.DecodeConfig(bytes.NewReader(preview.Data))
		if len(preview.Data) > cache.MaxPreviewBytes || decodeErr != nil || format != "png" || preview.MIMEType != "image/png" || config.Width != preview.Width || config.Height != preview.Height || config.Width < 1 || config.Height < 1 || config.Width > cache.MaxPreviewDimension || config.Height > cache.MaxPreviewDimension {
			return PreviewResult{Error: "The service returned an invalid image preview.", Code: "preview_invalid"}
		}
	} else {
		preview.Data = nil
	}
	return PreviewResult{Preview: &preview}
}

type OpenResult struct {
	Opened bool   `json:"opened"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

// OpenCurrent derives a target from service-owned metadata. The webview can
// choose an action and an image identity, but cannot supply arbitrary locations.
func (c *Client) OpenCurrent(imageID int64, target string) OpenResult {
	if target != "page" && target != "folder" {
		return OpenResult{Error: "Choose the original page or containing folder.", Code: "invalid_request"}
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	status, err := control.Request(ctx, c.socket, "")
	if err != nil {
		return OpenResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	if status.Current == nil || imageID < 1 || status.Current.ImageID != imageID {
		return OpenResult{Error: "The current image changed. Refresh status before opening it.", Code: "stale_image"}
	}
	entry := currentHistoryEntry(status)
	if entry == nil {
		return OpenResult{Error: "Current image details are unavailable. Refresh status to try again.", Code: "target_unavailable"}
	}
	location, err := openLocation(*entry, target)
	if err != nil {
		return OpenResult{Error: err.Error(), Code: "target_unavailable"}
	}
	latest, err := control.Request(ctx, c.socket, "")
	if err != nil {
		return OpenResult{Error: err.Error(), Code: requestErrorCode(err)}
	}
	if latest.InstanceID != status.InstanceID || latest.Current == nil || latest.Current.ID != entry.ID || latest.Current.ImageID != imageID {
		return OpenResult{Error: "The current image changed. Refresh status before opening it.", Code: "stale_image"}
	}
	if c.opener == nil {
		return OpenResult{Error: "Opening locations is unavailable in this client.", Code: "unsupported"}
	}
	if err := c.opener(ctx, location); err != nil {
		return OpenResult{Error: "The location could not be opened. It may no longer be available.", Code: "open_failed"}
	}
	return OpenResult{Opened: true}
}

func openLocation(entry playback.HistoryEntry, target string) (string, error) {
	if target == "page" {
		if entry.Attribution != nil {
			location := entry.Attribution.PageURL
			parsed, err := url.Parse(location)
			if err == nil && len(location) <= 4096 && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && !strings.ContainsAny(location, "\r\n\t ") {
				return location, nil
			}
		}
		return "", fmt.Errorf("This image has no available original page.")
	}
	path := entry.OriginalPath
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("This image has no available original folder.")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("The original image is no longer available in its folder.")
	}
	return filepath.Dir(filepath.Clean(path)), nil
}

func requestErrorCode(err error) string {
	var serviceError *control.Error
	if errors.As(err, &serviceError) {
		return serviceError.Code
	}
	return "request_failed"
}
