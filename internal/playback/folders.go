package playback

import (
	"context"
	"path/filepath"
	"strings"
)

func (f FolderConfig) enabled() bool { return f.Enabled == nil || *f.Enabled }

func (f FolderConfig) depth(fallback int) int {
	if f.MaxDepth != nil {
		return *f.MaxDepth
	}
	return fallback
}

func (f FolderConfig) label() string {
	if f.Name != "" {
		return f.Name
	}
	return filepath.Base(f.Path)
}

func validFolderPath(path string) bool {
	return filepath.IsAbs(path) && len(path) <= 4096 && !strings.ContainsRune(path, 0)
}

type UpdateFolderRequest struct {
	ImageRules      *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules    *bool             `json:"inherit_rules,omitempty"`
	Revision        string            `json:"revision"`
	ID              string            `json:"id"`
	Name            *string           `json:"name,omitempty"`
	Path            *string           `json:"path,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	MaxDepth        *int              `json:"max_depth,omitempty"`
	RefreshInterval *string           `json:"refresh_interval,omitempty"`
}

type RemoveFolderRequest struct {
	Revision string `json:"revision"`
	ID       string `json:"id"`
}

// UpdateFolder keeps the input ID stable. A depth of -1 removes the override;
// an empty name/refresh interval restores the basename/provider default.
func (s *Service) UpdateFolder(ctx context.Context, request UpdateFolderRequest) (Configuration, error) {
	if request.Name == nil && request.Path == nil && request.Enabled == nil && request.MaxDepth == nil && request.RefreshInterval == nil && request.ImageRules == nil && request.InheritRules == nil {
		return Configuration{}, &Error{Code: "invalid_request", Message: "provide at least one folder setting to update"}
	}
	if request.MaxDepth != nil && *request.MaxDepth < -1 {
		return Configuration{}, &Error{Code: "invalid_settings", Message: "folder max_depth must be non-negative or -1 to inherit"}
	}
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		provider, index := findFolder(config, request.ID)
		if index < 0 {
			return false, &Error{Code: "folder_not_found", Message: "local folder no longer exists; reload settings"}
		}
		folder := &provider.Folders[index]
		changed, rulesErr := editSourceRules(&folder.ImageRules, request.ImageRules, request.InheritRules)
		if rulesErr != nil {
			return false, rulesErr
		}
		if request.Path != nil {
			if !validFolderPath(*request.Path) {
				return false, &Error{Code: "invalid_settings", Message: "folder path must be absolute, contain no NUL and be at most 4096 bytes"}
			}
			root := filepath.Clean(*request.Path)
			for i, other := range provider.Folders {
				if root != folder.Path && i != index && other.Path == root {
					return false, &Error{Code: "invalid_settings", Message: "another local folder already uses that path"}
				}
			}
			changed = setPreference(&folder.Path, &root) || changed
		}
		if request.Name != nil {
			name := strings.TrimSpace(*request.Name)
			changed = setPreference(&folder.Name, &name) || changed
		}
		if request.Enabled != nil && folder.enabled() != *request.Enabled {
			folder.Enabled = request.Enabled
			changed = true
		}
		if request.MaxDepth != nil {
			if *request.MaxDepth == -1 {
				changed = folder.MaxDepth != nil || changed
				folder.MaxDepth = nil
			} else if folder.MaxDepth == nil || *folder.MaxDepth != *request.MaxDepth {
				folder.MaxDepth = request.MaxDepth
				changed = true
			}
		}
		changed = setPreference(&folder.RefreshInterval, request.RefreshInterval) || changed
		return changed, nil
	})
}

func (s *Service) RemoveFolder(ctx context.Context, request RemoveFolderRequest) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		provider, index := findFolder(config, request.ID)
		if index < 0 {
			return false, &Error{Code: "folder_not_found", Message: "local folder no longer exists; reload settings"}
		}
		provider.Folders = append(provider.Folders[:index], provider.Folders[index+1:]...)
		return true, nil
	})
}

func findFolder(config *Config, id string) (*ProviderConfig, int) {
	for i := range config.Providers {
		provider := &config.Providers[i]
		if provider.ID != "local" {
			continue
		}
		for j, folder := range provider.Folders {
			if folder.ID == id {
				return provider, j
			}
		}
	}
	return nil, -1
}
