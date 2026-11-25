package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// CapabilityFailure carries a public reason without exposing command output.
// The cause remains available to Go callers through errors.Is/errors.As.
type CapabilityFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	cause   error
}

func (e *CapabilityFailure) Error() string { return e.Message }
func (e *CapabilityFailure) Unwrap() error { return e.cause }

type BackendReadiness struct {
	Name    string             `json:"name"`
	Ready   bool               `json:"ready"`
	Failure *CapabilityFailure `json:"failure,omitempty"`
}

type PresentationCapability struct {
	Fit       string `json:"fit"`
	Available bool   `json:"available"`
	Upscale   bool   `json:"upscale"`
	Reason    string `json:"reason,omitempty"`
}

type DisplayGeometry struct {
	Name      string  `json:"name"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Scale     float64 `json:"scale"`
	Transform int     `json:"transform"`
}

type Geometry struct {
	State    string             `json:"state"`
	Displays []DisplayGeometry  `json:"displays"`
	Failure  *CapabilityFailure `json:"failure,omitempty"`
}

// Capabilities describes command readiness and implementation scope, not screen
// pixels or desktop acceptance. Geometry is a snapshot and may change afterward.
type Capabilities struct {
	Version            int                      `json:"version"`
	RequestedBackend   string                   `json:"requested_backend"`
	Backend            string                   `json:"backend,omitempty"`
	State              string                   `json:"state"`
	Backends           []BackendReadiness       `json:"backends"`
	Presentations      []PresentationCapability `json:"presentations"`
	AssignmentScope    string                   `json:"assignment_scope"`
	IndependentOutputs bool                     `json:"independent_outputs"`
	Geometry           Geometry                 `json:"geometry"`
}

// InspectCapabilities probes only read-only commands. Each readiness/geometry
// operation has a three-second budget, bounded output and joined child process.
func InspectCapabilities(ctx context.Context, name string) (Capabilities, error) {
	return inspectCapabilities(ctx, name, wallpaperBackends(), hyprlandGeometry, macOSGeometry)
}

func inspectCapabilities(ctx context.Context, name string, options []Selection, geometry, macGeometry func(context.Context) (Geometry, error)) (Capabilities, error) {
	result := Capabilities{Version: 1, RequestedBackend: name, State: "unavailable", Backends: []BackendReadiness{}, Presentations: []PresentationCapability{}, AssignmentScope: "unavailable", Geometry: Geometry{State: "unavailable", Displays: []DisplayGeometry{}}}
	if err := ValidateBackendName(name); err != nil {
		return result, err
	}
	var ready []Selection
	for _, option := range options {
		if name != "auto" && option.Name != name {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := option.Backend.Probe(probeCtx)
		cancel()
		check := BackendReadiness{Name: option.Name, Ready: err == nil}
		if err != nil {
			check.Failure = capabilityFailure("backend_unavailable", "desktop backend is unavailable", err)
		} else {
			ready = append(ready, option)
		}
		result.Backends = append(result.Backends, check)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(ready) == 0 {
		return result, nil
	}
	if len(ready) > 1 {
		result.State = "ambiguous"
		result.Geometry.State = "ambiguous"
		result.Geometry.Failure = capabilityFailure("backend_ambiguous", "choose an explicit desktop backend before using its geometry", nil)
		return result, nil
	}
	selected := ready[0]
	result.Backend = selected.Name
	result.State = "ready"
	result.AssignmentScope = "all_monitors"
	switch selected.Name {
	case "omarchy":
		queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		detected, err := geometry(queryCtx)
		cancel()
		result.Geometry = detected
		if err != nil {
			result.Geometry = Geometry{State: "unavailable", Displays: []DisplayGeometry{}, Failure: capabilityFailure("geometry_unavailable", "could not detect display geometry", err)}
		}
		contain := result.Geometry.State == "known" && len(result.Geometry.Displays) == 1 && result.Geometry.Displays[0].Transform == 0
		reason := ""
		if !contain {
			reason = "centered containment requires one unrotated monitor with valid geometry"
		}
		result.Presentations = []PresentationCapability{{Fit: "auto", Available: contain, Upscale: contain, Reason: reason}, {Fit: "contain", Available: contain, Upscale: contain, Reason: reason}, {Fit: "crop", Available: true}}
	case "noctalia":
		result.Geometry = unsupportedGeometry("Noctalia display geometry is not exposed by this adapter")
		result.Presentations = []PresentationCapability{{Fit: "auto", Available: true}, {Fit: "contain", Reason: "presentation is controlled by Noctalia settings"}, {Fit: "crop", Reason: "presentation is controlled by Noctalia settings"}}
	case "macos":
		result.AssignmentScope = "all_connected_screens_current_spaces"
		queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		detected, err := macGeometry(queryCtx)
		cancel()
		result.Geometry = detected
		if err != nil {
			result.Geometry = Geometry{State: "unavailable", Displays: []DisplayGeometry{}, Failure: capabilityFailure("geometry_unavailable", "could not detect macOS backing-pixel geometry; rebuild the companion helper if it predates geometry support", err)}
		}
		result.Presentations = []PresentationCapability{{Fit: "auto", Available: true, Upscale: true}, {Fit: "contain", Available: true, Upscale: true}, {Fit: "crop", Available: true}}
	}
	if result.Geometry.State != "known" || selected.Name == "omarchy" && !result.Presentations[0].Available {
		result.State = "partial"
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func unsupportedGeometry(message string) Geometry {
	return Geometry{State: "unsupported", Displays: []DisplayGeometry{}, Failure: capabilityFailure("geometry_unsupported", message, nil)}
}

func capabilityFailure(code, message string, cause error) *CapabilityFailure {
	if errors.Is(cause, context.DeadlineExceeded) {
		code = "timeout"
		message = "desktop query timed out"
	} else if errors.Is(cause, context.Canceled) {
		code = "canceled"
		message = "desktop query canceled"
	}
	return &CapabilityFailure{Code: code, Message: message, cause: cause}
}

func hyprlandGeometry(ctx context.Context) (Geometry, error) {
	output, err := runBackendCommand(ctx, "hyprctl", "monitors", "-j")
	if err != nil {
		return Geometry{}, err
	}
	return parseHyprlandGeometry([]byte(output))
}

func parseHyprlandGeometry(data []byte) (Geometry, error) {
	var monitors []struct {
		DisplayGeometry
		Disabled bool `json:"disabled"`
	}
	if err := json.Unmarshal(data, &monitors); err != nil {
		return Geometry{}, fmt.Errorf("decode display geometry: %w", err)
	}
	geometry := Geometry{State: "known", Displays: []DisplayGeometry{}}
	seen := map[string]bool{}
	for _, monitor := range monitors {
		if monitor.Disabled {
			continue
		}
		if monitor.Name == "" || len(monitor.Name) > 256 || seen[monitor.Name] || monitor.Scale <= 0 || math.IsNaN(monitor.Scale) || math.IsInf(monitor.Scale, 0) || monitor.Transform < 0 || monitor.Transform > 7 {
			return Geometry{}, fmt.Errorf("invalid display name, scale or transform")
		}
		if err := validateFitSize(monitor.Width, monitor.Height); err != nil {
			return Geometry{}, err
		}
		seen[monitor.Name] = true
		geometry.Displays = append(geometry.Displays, monitor.DisplayGeometry)
		if len(geometry.Displays) > 32 {
			return Geometry{}, fmt.Errorf("display count exceeds 32")
		}
	}
	switch len(geometry.Displays) {
	case 0:
		geometry.State = "unavailable"
		geometry.Failure = capabilityFailure("geometry_unavailable", "no active displays", nil)
	case 1:
	default:
		geometry.State = "ambiguous"
		geometry.Failure = capabilityFailure("geometry_ambiguous", "multiple active displays have no single assignment target", nil)
	}
	return geometry, nil
}
