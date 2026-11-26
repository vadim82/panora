package commons

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vadim82/panora/internal/source"
)

func (p *Provider) Suggest(ctx context.Context, params source.SuggestParams) ([]string, error) {
	prefix := strings.TrimSpace(params.Prefix)
	if params.Field != "category" || !params.Valid() || !source.ValidCommonsCategory(prefix) {
		return nil, source.Failure("invalid_config", "enter a Commons category prefix")
	}
	list := make([]string, 0, params.Limit)
	if utf8.RuneCountInString(prefix) < 2 {
		return list, nil
	}
	select {
	case p.suggestSlot <- struct{}{}:
		defer func() { <-p.suggestSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if time.Now().Before(p.suggestRetryUntil) {
		return list, nil
	}
	response, err := p.api(ctx, url.Values{
		"list": {"allcategories"}, "acprefix": {prefix}, "aclimit": {strconv.Itoa(params.Limit)},
		"acmin": {"1"}, "acprop": {"hidden"},
	})
	if err != nil {
		var failure *source.Error
		if errors.As(err, &failure) {
			p.suggestRetryUntil = retryUntil(failure)
		}
		return nil, err
	}
	if response.Query == nil || len(response.Query.Categories) > params.Limit {
		return nil, source.Failure("unavailable", "Commons category response exceeds limits or is incomplete")
	}
	seen := make(map[string]bool, len(response.Query.Categories))
	for _, category := range response.Query.Categories {
		if !category.Hidden && !seen[category.Name] && source.ValidCommonsCategory(category.Name) {
			list = append(list, category.Name)
			seen[category.Name] = true
		}
	}
	return list, nil
}

func retryUntil(failure *source.Error) time.Time {
	if failure.RetryAfterSeconds != nil {
		return time.Now().Add(time.Duration(*failure.RetryAfterSeconds) * time.Second)
	}
	if failure.Code == "rate_limited" {
		return time.Now().Add(5 * time.Second)
	}
	return time.Time{}
}
