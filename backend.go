package checkin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Backend interface {
	Ticket(context.Context, string) (Ticket, error)
	Context(context.Context, string) (EventContext, error)
	Use(context.Context, string, UseInput) (UseResult, error)
	Usages(context.Context, string) ([]Usage, error)
	Count(context.Context, string) (Counts, error)
}
type HTTPBackend struct {
	config Config
	client *http.Client
}

func NewHTTPBackend(config Config) *HTTPBackend {
	return &HTTPBackend{config: config, client: &http.Client{Timeout: config.HTTPTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (b *HTTPBackend) request(ctx context.Context, method, path string, input, output any) error {
	var raw []byte
	var err error
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			return &UpstreamError{Status: 503}
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.config.TicketsURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return &UpstreamError{Status: 503}
	}
	request.Header.Set("X-Internal-Key", b.config.InternalAPIKey)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return &UpstreamError{Status: 503}
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return &UpstreamError{Status: 503}
	}
	if response.StatusCode != 200 {
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			UsedAt *time.Time `json:"usedAt"`
		}
		_ = json.Unmarshal(raw, &body)
		return &UpstreamError{Status: response.StatusCode, Code: body.Error.Code, UsedAt: body.UsedAt}
	}
	if json.Unmarshal(raw, output) != nil {
		return &UpstreamError{Status: 503}
	}
	return nil
}
func (b *HTTPBackend) Ticket(ctx context.Context, code string) (Ticket, error) {
	var result Ticket
	err := b.request(ctx, "GET", "/internal/tickets/by-code/"+url.PathEscape(code), nil, &result)
	return result, err
}
func (b *HTTPBackend) Context(ctx context.Context, id string) (EventContext, error) {
	var result EventContext
	err := b.request(ctx, "GET", "/internal/tickets/events/"+url.PathEscape(id)+"/context", nil, &result)
	return result, err
}
func (b *HTTPBackend) Use(ctx context.Context, code string, input UseInput) (UseResult, error) {
	var result UseResult
	err := b.request(ctx, "POST", "/internal/tickets/by-code/"+url.PathEscape(code)+"/use", input, &result)
	return result, err
}
func (b *HTTPBackend) Usages(ctx context.Context, id string) ([]Usage, error) {
	var result []Usage
	err := b.request(ctx, "GET", "/internal/tickets/events/"+url.PathEscape(id)+"/usages", nil, &result)
	if err == nil && result == nil {
		err = &UpstreamError{Status: 503}
	}
	return result, err
}
func (b *HTTPBackend) Count(ctx context.Context, id string) (Counts, error) {
	var result Counts
	err := b.request(ctx, "GET", "/internal/tickets/count?eventId="+url.QueryEscape(id), nil, &result)
	return result, err
}
