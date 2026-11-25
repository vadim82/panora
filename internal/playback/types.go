// Package playback owns local selection, eligibility, history, persistence, and rotation.
// It has no CLI or HTTP dependencies; adapters translate its status and actions.
package playback

import (
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

const StatusVersion = 1
const RefreshHealthVersion = 1

// Counts describe metadata committed during one attempt, not decoded images.
// A null eligible count means actual-byte eligibility is unknown for this scope.
type RefreshCounts struct {
	Scope         string `json:"scope"`
	Pages         int    `json:"pages"`
	Inspected     int    `json:"inspected"`
	KnownEligible *int   `json:"known_eligible"`
}

type RefreshFailure struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RetryHealth struct {
	Scope          string         `json:"scope"`
	Attempts       int            `json:"attempts"`
	NextRetry      time.Time      `json:"next_retry"`
	Failure        RefreshFailure `json:"failure"`
	AffectedImages int            `json:"affected_images,omitempty"`
}

type InputRefreshHealth struct {
	Retries     []RetryHealth   `json:"retries,omitempty"`
	ID          string          `json:"id"`
	Revision    string          `json:"revision"`
	State       string          `json:"state"`
	Interval    string          `json:"interval"`
	LastAttempt *time.Time      `json:"last_attempt"`
	LastSuccess *time.Time      `json:"last_success"`
	NextRefresh *time.Time      `json:"next_refresh"`
	Counts      *RefreshCounts  `json:"counts"`
	Failure     *RefreshFailure `json:"failure,omitempty"`
}

type ProviderRefreshHealth struct {
	Retries       []RetryHealth        `json:"retries,omitempty"`
	ID            string               `json:"id"`
	Enabled       bool                 `json:"enabled"`
	State         string               `json:"state"`
	TotalInputs   int                  `json:"total_inputs"`
	OmittedInputs int                  `json:"omitted_inputs"`
	Inputs        []InputRefreshHealth `json:"inputs"`
}

// HistoryReference identifies a retained entry and its catalog image.
type HistoryReference struct {
	ID              string `json:"id"`
	ImageID         int64  `json:"image_id"`
	ContentRevision string `json:"content_revision,omitempty"`
}

// Current is the last accepted request at the history cursor, not verified pixels.
type Status struct {
	preparedNextTheme          string
	previewNeighbors           [4]neighborPreviewEntry
	previewExclusions          catalog.ExclusionSet
	OmarchyThemeRevision       string                  `json:"omarchy_theme_revision,omitempty"`
	OmarchyThemeName           string                  `json:"omarchy_theme_name,omitempty"`
	OmarchyThemeIssue          string                  `json:"omarchy_theme_issue,omitempty"`
	Version                    int                     `json:"version"`
	InstanceID                 string                  `json:"instance_id,omitempty"`
	SetupRequired              bool                    `json:"setup_required"`
	CurrentExcluded            bool                    `json:"current_excluded"`
	ExclusionUndoAvailable     bool                    `json:"exclusion_undo_available"`
	Paused                     bool                    `json:"paused"`
	Busy                       bool                    `json:"busy"`
	Uncertain                  bool                    `json:"uncertain"`
	Interval                   string                  `json:"interval"`
	NextAt                     *time.Time              `json:"next_at,omitempty"`
	Current                    *Entry                  `json:"current,omitempty"`
	CurrentPreviewContent      string                  `json:"current_preview_content,omitempty"`
	CurrentImage               *CurrentImage           `json:"current_image,omitempty"`
	HistoryLength              int                     `json:"history_length"`
	NavigationSupported        bool                    `json:"navigation_supported,omitempty"`
	PreviousBeyond             *HistoryReference       `json:"previous_beyond,omitempty"`
	ForwardBeyond              *HistoryReference       `json:"forward_beyond,omitempty"`
	PreparedBeyond             *HistoryReference       `json:"prepared_beyond,omitempty"`
	PreviousHistory            *HistoryReference       `json:"previous_history,omitempty"`
	PreparedNextConfigRevision string                  `json:"prepared_next_config_revision,omitempty"`
	PreparedNextRevision       uint64                  `json:"prepared_next_revision,omitempty"`
	PreparedNextPreviewContent string                  `json:"prepared_next_preview_content,omitempty"`
	PreparedNextImageID        int64                   `json:"prepared_next_image_id,omitempty"`
	PendingPreviewContent      string                  `json:"pending_preview_content,omitempty"`
	PendingImageID             int64                   `json:"pending_image_id,omitempty"`
	ForwardHistory             *HistoryReference       `json:"forward_history,omitempty"`
	HistoryPreviewRevision     uint64                  `json:"history_preview_revision,omitempty"`
	HistoryCursor              int                     `json:"history_cursor"`
	Backend                    string                  `json:"requested_backend"`
	LastError                  *Error                  `json:"last_error,omitempty"`
	Warnings                   []string                `json:"warnings,omitempty"`
	Refreshing                 bool                    `json:"refreshing"`
	RefreshWarnings            []string                `json:"refresh_warnings,omitempty"`
	RefreshHealthVersion       int                     `json:"refresh_health_version"`
	Providers                  []ProviderRefreshHealth `json:"providers,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }
