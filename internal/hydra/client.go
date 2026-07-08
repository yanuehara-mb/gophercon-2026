package hydra

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type Introspector interface {
	Introspect(ctx context.Context, token string) (*http.Response, error)
}

type Client struct {
	hydraURL   string
	httpClient *http.Client
}

func NewClient(hydraURL string) *Client {
	return &Client{
		hydraURL:   hydraURL,
		httpClient: &http.Client{},
	}
}

func (c *Client) Introspect(ctx context.Context, token string) (*http.Response, error) {
	form := url.Values{"token": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.hydraURL+"/admin/oauth2/introspect",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.httpClient.Do(req)
}
