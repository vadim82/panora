package playback

import (
	"context"
	"errors"

	"github.com/vadim82/panora/internal/source"
)

type commonsSearcher interface {
	Search(context.Context, source.CommonsSearch) (source.CommonsSearchPage, error)
}

// SearchCommons is a user-requested preview. It never changes source settings,
// writes discovery candidates, or authorizes a wallpaper.
func (s *Service) SearchCommons(ctx context.Context, query source.CommonsSearch) (source.CommonsSearchPage, error) {
	provider, ok := s.request.provider(source.CommonsID).(commonsSearcher)
	if !ok {
		return source.CommonsSearchPage{}, &Error{Code: "unsupported", Message: "this service does not support Commons search"}
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	page, err := provider.Search(ctx, query)
	if err == nil {
		return page, nil
	}
	var failure *source.Error
	if errors.As(err, &failure) {
		return page, &Error{Code: failure.Code, Message: failure.Message}
	}
	return page, &Error{Code: "unavailable", Message: "Commons search could not be completed"}
}
