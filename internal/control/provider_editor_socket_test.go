//go:build linux || darwin

package control

import (
	"context"
	"slices"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

type suggestFixture struct {
	testService
	providerInputService
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
