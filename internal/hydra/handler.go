package hydra

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	introspector Introspector
}

func NewHandler(i Introspector) *Handler {
	return &Handler{introspector: i}
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
