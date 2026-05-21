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

// authorize checks the incoming context for a valid
// `authorization: Bearer <token>` header. Shared by the unary and
// stream interceptors. gRPC lower-cases metadata keys; the scheme
// prefix is matched case-insensitively per RFC 6750.
func authorize(ctx context.Context, expected []byte) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}
	raw := values[0]
	const prefix = "bearer "
	if len(raw) <= len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return status.Error(codes.Unauthenticated, "authorization scheme must be Bearer")
	}
	got := []byte(strings.TrimSpace(raw[len(prefix):]))
	if subtle.ConstantTimeEq(int32(len(got)), int32(len(expected))) != 1 ||
		subtle.ConstantTimeCompare(got, expected) != 1 {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
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
		if err := authorize(ctx, expectedBytes); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// newAuthStreamInterceptor is the streaming counterpart of
// newAuthInterceptor — the same bearer-token rule applied to streaming
// RPCs such as WatchResources. The health service stays exempt.
func newAuthStreamInterceptor(expected string) grpc.StreamServerInterceptor {
	expectedBytes := []byte(expected)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if strings.HasPrefix(info.FullMethod, healthServicePrefix) {
			return handler(srv, ss)
		}
		if err := authorize(ss.Context(), expectedBytes); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
