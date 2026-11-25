// Package control implements Panora's private, versioned Unix-socket API.
package control

import (
	"context"

	"github.com/vadim82/panora/internal/playback"
)

const APIVersion = playback.StatusVersion

type Entry = playback.Entry
type Status = playback.Status
type Error = playback.Error

type Service interface {
	Status() Status
	Action(context.Context, string) (Status, error)
	Exclusions(context.Context, int64, int) (playback.ExclusionPage, error)
	RestoreExclusion(context.Context, int64) (Status, error)
}

func validAction(action string) bool {
	switch action {
	case "next", "previous", "pause", "resume", "exclude", "undo-exclude":
		return true
	}
	return false
}
