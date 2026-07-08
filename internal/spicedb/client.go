package spicedb

import (
	"context"
	"fmt"
	"strings"

	authzed "github.com/authzed/authzed-go/v1"
	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Checker interface {
	Check(ctx context.Context, subject, object, permission string) (bool, error)
}

type Client struct {
	authzed *authzed.Client
	cache   TokenCache
}

func NewClient(addr, token string, cache TokenCache) (*Client, error) {
	c, err := authzed.NewClient(addr,
		grpcutil.WithInsecureBearerToken(token),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	return &Client{authzed: c, cache: cache}, nil
}

func (c *Client) Check(ctx context.Context, subject, object, permission string) (bool, error) {
	subjectType, subjectID, err := parseRef(subject)
	if err != nil {
		return false, fmt.Errorf("invalid subject: %w", err)
	}
	objectType, objectID, err := parseRef(object)
	if err != nil {
		return false, fmt.Errorf("invalid object: %w", err)
	}

	resp, err := c.authzed.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Consistency: selectConsistency(ctx, c.cache),
		Resource: &v1.ObjectReference{
			ObjectType: objectType,
			ObjectId:   objectID,
		},
		Permission: permission,
		Subject: &v1.SubjectReference{
			Object: &v1.ObjectReference{
				ObjectType: subjectType,
				ObjectId:   subjectID,
			},
		},
	})
	if err != nil {
		return false, err
	}

	if resp.CheckedAt != nil {
		_ = c.cache.Set(ctx, resp.CheckedAt.Token)
	}

	return resp.Permissionship == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, nil
}

// selectConsistency returns AtLeastAsFresh if a cached ZedToken exists, FullyConsistent otherwise.
// Cache errors are silently ignored — FullyConsistent is a safe fallback.
func selectConsistency(ctx context.Context, cache TokenCache) *v1.Consistency {
	if token, err := cache.Get(ctx); err == nil && token != "" {
		return &v1.Consistency{
			Requirement: &v1.Consistency_AtLeastAsFresh{
				AtLeastAsFresh: &v1.ZedToken{Token: token},
			},
		}
	}
	return &v1.Consistency{
		Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true},
	}
}

func parseRef(s string) (objectType, objectID string, err error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid reference format %q, expected type:id", s)
	}
	return parts[0], parts[1], nil
}
