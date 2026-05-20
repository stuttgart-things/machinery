package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	defaultAuthTokenEnvVar = "MACHINERY_AUTH_TOKEN"
	healthServicePrefix    = "/grpc.health.v1.Health/"
)

// resolveAuthToken returns the token machinery should accept on the gRPC
// network path. Order: inline cfg.Token → cfg.TokenFile contents →
// $cfg.TokenEnvVar → $MACHINERY_AUTH_TOKEN. Whitespace is trimmed.
// Returns ("", nil) when no token is configured anywhere — callers
// decide whether that is fatal (it is when AuthConfig.Enabled is true).
func resolveAuthToken(cfg AuthConfig) (string, error) {
	if cfg.Token != "" {
		return strings.TrimSpace(cfg.Token), nil
	}
	if cfg.TokenFile != "" {
		data, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return "", fmt.Errorf("reading auth token file %q: %w", cfg.TokenFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if cfg.TokenEnvVar != "" {
		if v := strings.TrimSpace(os.Getenv(cfg.TokenEnvVar)); v != "" {
			return v, nil
		}
	}
	if v := strings.TrimSpace(os.Getenv(defaultAuthTokenEnvVar)); v != "" {
		return v, nil
	}
	return "", nil
}

// newAuthInterceptor returns a UnaryServerInterceptor that requires
// `authorization: Bearer <token>` metadata on every RPC except those
// under /grpc.health.v1.Health/ (LB/k8s probes stay anonymous).
// The expected token is captured at construction time, so callers can
// keep it out of long-lived globals.
func newAuthInterceptor(expected string) grpc.UnaryServerInterceptor {
	expectedBytes := []byte(expected)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, healthServicePrefix) {
			return handler(ctx, req)
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		values := md.Get("authorization")
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}
		// gRPC normalizes keys to lower-case but the scheme prefix is
		// case-insensitive per RFC 6750; match either Bearer or bearer.
		raw := values[0]
		const prefix = "bearer "
		if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
			return nil, status.Error(codes.Unauthenticated, "authorization scheme must be Bearer")
		}
		got := []byte(strings.TrimSpace(raw[len(prefix):]))
		if subtle.ConstantTimeEq(int32(len(got)), int32(len(expectedBytes))) != 1 ||
			subtle.ConstantTimeCompare(got, expectedBytes) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}
		return handler(ctx, req)
	}
}
