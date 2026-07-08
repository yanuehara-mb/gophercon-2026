package spicedb

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	checker Checker
}

func NewHandler(c Checker) *Handler {
	return &Handler{checker: c}
}

type verifyRequest struct {
	Subject    string `json:"subject"    binding:"required"`
	Object     string `json:"object"     binding:"required"`
	Permission string `json:"permission" binding:"required"`
}

func (h *Handler) Verify(c *gin.Context) {
	var req verifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, _, err := parseRef(req.Subject); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subject: " + err.Error()})
		return
	}
	if _, _, err := parseRef(req.Object); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid object: " + err.Error()})
		return
	}

	allowed, err := h.checker.Check(c.Request.Context(), req.Subject, req.Object, req.Permission)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream error"})
		return
	}

	if allowed {
		c.Status(http.StatusOK)
	} else {
		c.Status(http.StatusForbidden)
	}
}
