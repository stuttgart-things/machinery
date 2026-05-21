package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testToken = "s3cr3t-test-token"

func okHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

func TestAuthInterceptor_MissingMetadata_Unauthenticated(t *testing.T) {
	interceptor := newAuthInterceptor(testToken)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/Foo/Bar"}, okHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated; err=%v", got, err)
	}
}

func TestAuthInterceptor_MissingHeader_Unauthenticated(t *testing.T) {
	interceptor := newAuthInterceptor(testToken)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-other", "value"))
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/Foo/Bar"}, okHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated", got)
	}
}

func TestAuthInterceptor_BadScheme_Unauthenticated(t *testing.T) {
	interceptor := newAuthInterceptor(testToken)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic "+testToken))
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/Foo/Bar"}, okHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated", got)
	}
}

func TestAuthInterceptor_BadToken_Unauthenticated(t *testing.T) {
	interceptor := newAuthInterceptor(testToken)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong-token"))
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/Foo/Bar"}, okHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated", got)
	}
}

func TestAuthInterceptor_ValidToken_PassesThrough(t *testing.T) {
	for _, scheme := range []string{"Bearer ", "bearer "} {
		t.Run(scheme, func(t *testing.T) {
			interceptor := newAuthInterceptor(testToken)
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", scheme+testToken))
			resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/Foo/Bar"}, okHandler)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp != "ok" {
				t.Fatalf("got resp %v, want ok", resp)
			}
		})
	}
}

func TestAuthInterceptor_HealthExempt(t *testing.T) {
	interceptor := newAuthInterceptor(testToken)
	// No metadata at all; the health probe must still succeed.
	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, okHandler)
	if err != nil {
		t.Fatalf("health check should bypass auth, got error: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("got resp %v, want ok", resp)
	}
}

func TestResolveAuthToken_Inline(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "")
	got, err := resolveAuthToken(AuthConfig{Token: "  inline-token\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "inline-token" {
		t.Fatalf("got %q, want %q", got, "inline-token")
	}
}

func TestResolveAuthToken_FromFile(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveAuthToken(AuthConfig{TokenFile: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "file-token" {
		t.Fatalf("got %q, want %q", got, "file-token")
	}
}

func TestResolveAuthToken_FromCustomEnv(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "")
	t.Setenv("CUSTOM_AUTH_TOKEN", "env-token")
	got, err := resolveAuthToken(AuthConfig{TokenEnvVar: "CUSTOM_AUTH_TOKEN"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-token" {
		t.Fatalf("got %q, want %q", got, "env-token")
	}
}

func TestResolveAuthToken_FromDefaultEnv(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "default-env-token")
	got, err := resolveAuthToken(AuthConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "default-env-token" {
		t.Fatalf("got %q, want %q", got, "default-env-token")
	}
}

func TestResolveAuthToken_Missing(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "")
	got, err := resolveAuthToken(AuthConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestResolveAuthToken_InlineBeatsEnv(t *testing.T) {
	t.Setenv(defaultAuthTokenEnvVar, "env-token")
	got, err := resolveAuthToken(AuthConfig{Token: "inline-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "inline-token" {
		t.Fatalf("got %q, want inline-token (env should not override an inline token)", got)
	}
}

// fakeServerStream is a grpc.ServerStream carrying a fixed context —
// enough for the stream interceptor, which only reads ss.Context().
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func okStreamHandler(_ any, _ grpc.ServerStream) error { return nil }

func TestAuthStreamInterceptor_ValidToken_PassesThrough(t *testing.T) {
	interceptor := newAuthStreamInterceptor(testToken)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+testToken))
	err := interceptor(nil, &fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/Foo/Watch"}, okStreamHandler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthStreamInterceptor_BadToken_Unauthenticated(t *testing.T) {
	interceptor := newAuthStreamInterceptor(testToken)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer wrong-token"))
	err := interceptor(nil, &fakeServerStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/Foo/Watch"}, okStreamHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated", got)
	}
}

func TestAuthStreamInterceptor_MissingMetadata_Unauthenticated(t *testing.T) {
	interceptor := newAuthStreamInterceptor(testToken)
	err := interceptor(nil, &fakeServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/Foo/Watch"}, okStreamHandler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("got code %v, want Unauthenticated", got)
	}
}

func TestAuthStreamInterceptor_HealthExempt(t *testing.T) {
	interceptor := newAuthStreamInterceptor(testToken)
	// No metadata at all; the health Watch stream must still pass.
	err := interceptor(nil, &fakeServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{FullMethod: "/grpc.health.v1.Health/Watch"}, okStreamHandler)
	if err != nil {
		t.Fatalf("health stream should bypass auth, got error: %v", err)
	}
}
