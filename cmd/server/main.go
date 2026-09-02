package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/yanuehara/gophercon-2026/internal/config"
	"github.com/yanuehara/gophercon-2026/internal/hydra"
	"github.com/yanuehara/gophercon-2026/internal/spicedb"
)

func main() {
	cfg := config.Load()

	hydraClient := hydra.NewClient(cfg.HydraURL, cfg.HydraPublicURL)
	hydraHandler := hydra.NewHandler(hydraClient, hydraClient)

	tokenCache := spicedb.NewTokenCache(cfg.RedisAddr)
	spicedbClient, err := spicedb.NewClient(cfg.SpiceDBAddr, cfg.SpiceDBToken, tokenCache)
	if err != nil {
		log.Fatalf("failed to connect to spicedb: %v", err)
	}
	spicedbHandler := spicedb.NewHandler(spicedbClient)

	r := gin.Default()
	r.POST("/oauth2/token", hydraHandler.Token)
	r.POST("/oauth2/introspect", hydraHandler.Introspect)
	r.POST("/authorization/verify", spicedbHandler.Verify)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
