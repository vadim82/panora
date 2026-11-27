//go:build !darwin

package appservice

import (
	"context"
	"errors"
)

func WatchHomebrewUpgrade(context.Context, string) <-chan string { return nil }

func ExecHomebrewService(string) error { return errors.ErrUnsupported }
