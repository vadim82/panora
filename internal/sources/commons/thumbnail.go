package commons

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/vadim82/panora/internal/source"
)

const (
	maxThumbnailBytes   = 160 << 10
	maxInlineThumbnail  = 56 << 10
	maxThumbnailAxis    = 512
	maxThumbnailWorkers = 3
)

func (p *Provider) resolveThumbnails(ctx context.Context, results []source.CommonsSearchResult, urls []string) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var work sync.WaitGroup
	for worker := range min(maxThumbnailWorkers, len(results)) {
		work.Go(func() {
			for index := worker; index < len(results) && ctx.Err() == nil; index += maxThumbnailWorkers {
				results[index].Thumbnail = p.thumbnail(ctx, urls[index])
			}
		})
	}
	work.Wait()
}

func (p *Provider) thumbnail(ctx context.Context, raw string) string {
	url := canonicalURL(raw, "upload.wikimedia.org", "/wikipedia/commons/thumb/", "thumbnail")
	if url == "" {
		url = canonicalURL(raw, "thumb.wikimedia.org", "/wikipedia/commons/thumb/", "thumbnail")
	}
	if url == "" {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", source.UserAgent)
	resp, err := p.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > maxThumbnailBytes {
		return ""
	}
	rawBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxThumbnailBytes+1))
	if err != nil || len(rawBytes) > maxThumbnailBytes {
		return ""
	}
	header, format, err := image.DecodeConfig(bytes.NewReader(rawBytes))
	if err != nil || (format != "jpeg" && format != "png") ||
		header.Width < 1 || header.Height < 1 || header.Width > maxThumbnailAxis || header.Height > maxThumbnailAxis {
		return ""
	}
	pixels, decoded, err := image.Decode(bytes.NewReader(rawBytes))
	if err != nil || format != decoded || pixels.Bounds().Size() != image.Pt(header.Width, header.Height) {
		return ""
	}
	if format == "png" {
		opaque := image.NewRGBA(pixels.Bounds())
		draw.Draw(opaque, opaque.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		draw.Draw(opaque, opaque.Bounds(), pixels, pixels.Bounds().Min, draw.Over)
		pixels = opaque
	}
	var clean bytes.Buffer
	if jpeg.Encode(&clean, pixels, &jpeg.Options{Quality: 77}) != nil || clean.Len() > maxInlineThumbnail {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(clean.Bytes())
}
