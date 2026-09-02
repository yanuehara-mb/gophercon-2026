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

// TokenIssuer forwards an OAuth2 token request upstream. The caller owns the
// form and the client authentication header so the facade stays transparent to
// whichever grant and client authentication method the client picked.
type TokenIssuer interface {
	Token(ctx context.Context, form url.Values, authorization string) (*http.Response, error)
}

type Client struct {
	hydraURL       string
	hydraPublicURL string
	httpClient     *http.Client
}

func NewClient(hydraURL, hydraPublicURL string) *Client {
	return &Client{
		hydraURL:       hydraURL,
		hydraPublicURL: hydraPublicURL,
		httpClient:     &http.Client{},
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

// Token targets Hydra's public API: the token endpoint is public by design,
// unlike introspection, which lives behind the admin API.
func (c *Client) Token(ctx context.Context, form url.Values, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.hydraPublicURL+"/oauth2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return c.httpClient.Do(req)
}
