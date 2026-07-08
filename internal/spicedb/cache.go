package spicedb

import (
	"context"

	"github.com/redis/go-redis/v9"
)

const zedTokenKey = "spicedb:zedtoken"

type TokenCache interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, token string) error
}

type redisCache struct {
	client *redis.Client
}

func NewTokenCache(addr string) *redisCache {
	return &redisCache{
		client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (c *redisCache) Get(ctx context.Context) (string, error) {
	val, err := c.client.Get(ctx, zedTokenKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (c *redisCache) Set(ctx context.Context, token string) error {
	return c.client.Set(ctx, zedTokenKey, token, 0).Err()
}
