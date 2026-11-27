//go:build linux || darwin

package control

import (
	"context"
	"slices"
	"testing"

	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/source"
)

type suggestFixture struct {
	testService
	providerInputService
}

type alphaEditorFixture struct{ testService }

func (*alphaEditorFixture) AlphaCoders(_ context.Context, query playback.AlphaRequest) (playback.AlphaResult, error) {
	if query.Action != "tags" || query.Prefix != "ne" {
		return playback.AlphaResult{}, &Error{Code: "invalid_request", Message: "unexpected Alpha Coders request"}
	}
	return playback.AlphaResult{
		Connected: true,
		Tags:      []source.AlphaTag{{ID: 17, Name: "Nebula", Total: 42}},
	}, nil
}

func TestAlphaCodersPrivateSocketRoundTrip(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&alphaEditorFixture{}))
	got, err := AlphaCoders(t.Context(), path, playback.AlphaRequest{Action: "tags", Prefix: "ne"})
	if err != nil || !got.Connected || !slices.Equal(got.Tags, []source.AlphaTag{{ID: 17, Name: "Nebula", Total: 42}}) {
		t.Fatalf("Alpha Coders tags=%+v err=%v", got, err)
	}
}

func (s *suggestFixture) SuggestProviderInput(_ context.Context, id string, query source.SuggestParams) ([]string, error) {
	if id != "fixture-images" || query != (source.SuggestParams{Field: "tags", Prefix: "fo", Limit: 10}) {
		return nil, &Error{Code: "invalid_request", Message: "fixture received unexpected suggestion arguments"}
	}
	return []string{"forest"}, nil
}

func TestProviderSuggestionsPrivateSocketRoundTrip(t *testing.T) {
	path := socketPath(t)
	serveFixture(t, path, Handler(&suggestFixture{}))
	query := ProviderSuggestRequest{
		ProviderID:    "fixture-images",
		SuggestParams: source.SuggestParams{Field: "tags", Prefix: "fo", Limit: 10},
	}
	got, err := SuggestProviderInput(t.Context(), path, query)
	if err != nil || got.Version != APIVersion || !slices.Equal(got.Values, []string{"forest"}) {
		t.Fatalf("suggestions=%+v err=%v", got, err)
	}
	query.Prefix = " "
	if _, err := SuggestProviderInput(t.Context(), path, query); err == nil {
		t.Fatal("accepted an empty suggestion prefix")
	}
}
