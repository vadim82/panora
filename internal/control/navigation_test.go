//go:build linux || darwin

package control

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/playback"
)

type navigationFixture struct {
	testService
	expected playback.NavigationRequest
	started  chan struct{}
	canceled chan struct{}
}

func (s *navigationFixture) Navigate(ctx context.Context, request playback.NavigationRequest) (Status, error) {
	if request != s.expected {
		return Status{}, &Error{Code: "fixture_input", Message: "navigation input changed during transport"}
	}
	close(s.started)
	select {
	case <-s.canceled:
		return Status{}, &Error{Code: "navigation_superseded", Message: "cancel reached this request while it was active"}
	case <-ctx.Done():
		return Status{}, ctx.Err()
	}
}

func (s *navigationFixture) CancelNavigation(instance, token string) error {
	if instance != s.expected.Instance || token != s.expected.Token {
		return &Error{Code: "fixture_input", Message: "cancellation input changed during transport"}
	}
	close(s.canceled)
	return nil
}

func TestNavigationCancelCanReachActiveSocketRequest(t *testing.T) {
	path := socketPath(t)
	request := playback.NavigationRequest{
		Instance: "service-instance", Current: "history_42", Token: strings.Repeat("b", 32), Offset: -2,
	}
	fixture := &navigationFixture{expected: request, started: make(chan struct{}), canceled: make(chan struct{})}
	serveFixture(t, path, Handler(fixture))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Navigate(ctx, path, request)
		result <- err
	}()
	select {
	case <-fixture.started:
	case <-ctx.Done():
		t.Fatal("navigation did not reach the service")
	}
	if err := CancelNavigation(ctx, path, request.Instance, request.Token); err != nil {
		t.Fatalf("could not cancel while navigation was in progress: %v", err)
	}
	select {
	case err := <-result:
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "navigation_superseded" {
			t.Fatalf("navigation error = %v, want navigation_superseded", err)
		}
	case <-ctx.Done():
		t.Fatal("navigation did not return after cancellation")
	}
}

func TestNavigationRejectsUnsupportedMethodsAndMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"method", http.MethodGet, navigationEndpoint, "", http.StatusMethodNotAllowed},
		{"extra query", http.MethodPost, navigationEndpoint + "?unexpected=1", "{}", http.StatusBadRequest},
		{"unknown field", http.MethodDelete, navigationEndpoint, `{"unexpected":1}`, http.StatusBadRequest},
		{"large body", http.MethodPost, navigationEndpoint, `{"token":"` + strings.Repeat("0", 512) + `"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := Handler(&navigationFixture{})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if response.Code != tc.want {
				t.Fatalf("response=%d %s, want %d", response.Code, response.Body.String(), tc.want)
			}
		})
	}
}
