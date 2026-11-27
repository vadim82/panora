// Package local is the bundled discovery plugin. It only lists local paths;
// image decoding, eligibility, random selection and rendering belong to the host.
package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vadim82/panora/internal/localfiles"
	"github.com/vadim82/panora/internal/source"
)

type Provider struct{}

func (Provider) Describe(context.Context) (source.Description, error) {
	return source.Description{Protocol: source.Protocol, ID: "local", Name: "Local folder", MediaKinds: []string{"image"}, ContentKinds: []string{"local"}, Operations: []string{"describe", "validate", "discover", "resolve"}, Fields: []source.Field{{Key: "root", Type: "text", Label: "Folder"}, {Key: "max_depth", Type: "integer", Label: "Subfolder depth"}}}, nil
}

func parseConfig(ctx context.Context, raw json.RawMessage) (source.LocalConfig, error) {
	var config source.LocalConfig
	if err := ctx.Err(); err != nil {
		return config, err
	}
	if err := source.Decode(raw, &config); err != nil {
		return config, source.Failure("invalid_config", "expected root and max_depth")
	}
	if !filepath.IsAbs(config.Root) || strings.ContainsRune(config.Root, 0) || config.MaxDepth < 0 {
		return config, source.Failure("invalid_config", "root must be a valid absolute path and max_depth non-negative")
	}
	config.Root = filepath.Clean(config.Root)
	info, err := os.Stat(config.Root)
	if err != nil {
		return config, source.Failure("unavailable", "could not access the folder")
	}
	if !info.IsDir() {
		return config, source.Failure("invalid_config", "root is not a directory")
	}
	return config, nil
}

func (Provider) Validate(ctx context.Context, raw json.RawMessage) error {
	_, err := parseConfig(ctx, raw)
	return err
}

type cursor struct {
	Config string `json:"config"`
	Offset int    `json:"offset"`
	Anchor string `json:"anchor"`
}

func (Provider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	page := source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage("null")}
	config, err := parseConfig(ctx, params.Config)
	if err != nil {
		return page, err
	}
	if params.Limit < 1 || params.Limit > source.MaxPageSize {
		return page, source.Failure("invalid_config", "limit must be between 1 and 100")
	}
	encoded, _ := json.Marshal(config)
	digest := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(digest[:])
	position := cursor{Config: fingerprint}
	if len(params.Cursor) > source.MaxCursorBytes {
		return page, source.Failure("invalid_cursor", "cursor is too large")
	}
	if len(params.Cursor) != 0 && !source.IsNull(params.Cursor) {
		if err := source.Decode(params.Cursor, &position); err != nil || position.Config != fingerprint || position.Offset < 1 || position.Offset > 100000 || len(position.Anchor) != sha256.Size*2 {
			return page, source.Failure("invalid_cursor", "cursor does not match this folder configuration")
		}
	}
	index, more := 0, false
	matched := position.Offset == 0
	var lastPath string
	err = localfiles.Walk(ctx, config.Root, config.MaxDepth, func(path string) error {
		index++
		if index < position.Offset {
			return nil
		}
		if index == position.Offset {
			matched = pathAnchor(path) == position.Anchor
			if !matched {
				return fs.SkipAll
			}
			return nil
		}
		if len(page.Candidates) == params.Limit {
			more = true
			return fs.SkipAll
		}
		relative, err := filepath.Rel(config.Root, path)
		if err != nil {
			return err
		}
		lastPath = path
		id := filepath.ToSlash(relative)
		ref, _ := json.Marshal(struct {
			Path string `json:"path"`
		}{path})
		page.Candidates = append(page.Candidates, source.Candidate{ID: id, MediaKind: "image", Ref: ref, Content: &source.Content{MediaKind: "image", Kind: "local", Path: path}})
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return page, ctx.Err()
		}
		return page, source.Failure("unavailable", "could not finish reading the folder")
	}
	if !matched {
		return page, source.Failure("invalid_cursor", "folder changed; restart discovery")
	}
	if more {
		page.NextCursor, _ = json.Marshal(cursor{Config: fingerprint, Offset: position.Offset + len(page.Candidates), Anchor: pathAnchor(lastPath)})
	}
	return page, nil
}

func pathAnchor(path string) string {
	digest := sha256.Sum256([]byte(path))
	return hex.EncodeToString(digest[:])
}

func (Provider) Resolve(ctx context.Context, params source.ResolveParams) (source.Resolved, error) {
	var result source.Resolved
	config, err := parseConfig(ctx, params.Config)
	if err != nil {
		return result, err
	}
	var ref struct {
		Path string `json:"path"`
	}
	if err := source.Decode(params.Ref, &ref); err != nil {
		return result, source.Failure("invalid_config", "invalid local candidate reference")
	}
	path, err := localfiles.ContainedPath(config.Root, ref.Path, config.MaxDepth)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, source.Failure("not_found", "local image is no longer available")
		}
		return result, source.Failure("invalid_config", "local image is outside the source or is not a supported file")
	}
	relative, err := filepath.Rel(config.Root, path)
	if err != nil || params.ID == "" || filepath.ToSlash(relative) != params.ID {
		return result, source.Failure("invalid_config", "candidate ID does not match its reference")
	}
	result.Content = source.Content{MediaKind: "image", Kind: "local", Path: path}
	return result, nil
}
