package hydra

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Headers the OAuth2 token endpoint is required to carry back to the client:
// no-store/no-cache on every response (RFC 6749 §5.1) and the challenge on a
// failed client authentication (§5.2).
var tokenResponseHeaders = []string{
	"Content-Type",
	"Cache-Control",
	"Pragma",
	"WWW-Authenticate",
}

type Handler struct {
	introspector Introspector
	tokenIssuer  TokenIssuer
}

func NewHandler(i Introspector, t TokenIssuer) *Handler {
	return &Handler{introspector: i, tokenIssuer: t}
}

func (h *Handler) Introspect(c *gin.Context) {
	token := c.PostForm("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	resp, err := h.introspector.Introspect(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream error"})
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", resp.Header.Get("Content-Type"))
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}

// Token proxies the OAuth2 token endpoint without validating the request
// locally: any rejection has to be Hydra's, so clients get the canonical
// error object of RFC 6749 §5.2 instead of a facade-shaped one.
func (h *Handler) Token(c *gin.Context) {
	if err := c.Request.ParseForm(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "malformed form body",
		})
		return
	}

	// PostForm and not Form: the latter merges the query string in, which would
	// let a query parameter smuggle a scope or client_id upstream.
	resp, err := h.tokenIssuer.Token(c.Request.Context(), c.Request.PostForm,
		c.GetHeader("Authorization"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":             "server_error",
			"error_description": "token endpoint unavailable",
		})
		return
	}
	defer resp.Body.Close()

	for _, name := range tokenResponseHeaders {
		if v := resp.Header.Get(name); v != "" {
			c.Header(name, v)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body)
}
