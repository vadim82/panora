package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

func TestCapabilitiesInspectRunningChoiceWithoutMutatingPlayback(t *testing.T) {
	service, _, backend := configurationFixture(t, "version=1\n")
	service.request.InspectCapabilities = func(ctx context.Context, name string) (desktop.Capabilities, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("inspection has no deadline")
		}
		return testDesktopCapabilities(name), nil
	}
	before := service.Status()
	saved := settings(t, service)
	if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: saved.Revision, Backend: configValue("noctalia")}); err != nil {
		t.Fatal(err)
	}
	got, err := service.Capabilities(t.Context())
	if err != nil || got.RequestedBackend != "noctalia" || got.State != "partial" {
		t.Fatalf("capabilities=%+v %v", got, err)
	}
	after := service.Status()
	if backend.calls.Load() != 0 || before.HistoryLength != after.HistoryLength || before.Paused != after.Paused || before.SetupRequired != after.SetupRequired {
		t.Fatal("inspection changed playback")
	}
}

func TestCapabilityInspectionDoesNotBlockActionsAndStopsWithService(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	entered := make(chan struct{})
	service.request.InspectCapabilities = func(ctx context.Context, _ string) (desktop.Capabilities, error) {
		close(entered)
		<-ctx.Done()
		return desktop.Capabilities{}, ctx.Err()
	}
	result := make(chan error, 1)
	go func() { _, err := service.Capabilities(t.Context()); result <- err }()
	<-entered
	if _, err := service.Action(t.Context(), "pause"); err != nil {
		t.Fatalf("probe held action lock=%v", err)
	}
	service.Close()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("inspection continued after shutdown")
	}
}
