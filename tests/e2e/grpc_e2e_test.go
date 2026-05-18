//go:build e2e

// Package e2e runs against a live machinery deployment in kind, exercising
// the gRPC ResourceService end-to-end. Gated by `-tags=e2e` so `go test ./...`
// in normal CI does not try to dial a cluster.
//
// Expected env:
//   E2E_GRPC_ADDR  host:port of the machinery gRPC endpoint (e.g. localhost:50051)
//                  The Taskfile sets up a kubectl port-forward before invoking
//                  `go test`.
package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func dial(t *testing.T) (resourceservice.ResourceServiceClient, func()) {
	t.Helper()
	addr := os.Getenv("E2E_GRPC_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	return resourceservice.NewResourceServiceClient(conn), func() { _ = conn.Close() }
}

func TestE2E_GetResources_HarvesterVM(t *testing.T) {
	client, done := dial(t)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.GetResources(ctx, &resourceservice.ResourceRequest{
		Kind:  "HarvesterVM",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("GetResources(HarvesterVM): %v", err)
	}

	got := map[string]*resourceservice.ResourceStatus{}
	for _, r := range resp.Resources {
		got[r.Name] = r
	}

	ready, ok := got["e2e-vm-ready"]
	if !ok {
		t.Fatalf("expected fixture e2e-vm-ready in response, got %v", names(resp.Resources))
	}
	if !ready.Ready {
		t.Errorf("e2e-vm-ready should be Ready=true, got %+v", ready)
	}

	pending, ok := got["e2e-vm-pending"]
	if !ok {
		t.Fatalf("expected fixture e2e-vm-pending in response, got %v", names(resp.Resources))
	}
	if pending.Ready {
		t.Errorf("e2e-vm-pending should be Ready=false, got %+v", pending)
	}
}

func TestE2E_GetResources_Wildcard(t *testing.T) {
	client, done := dial(t)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.GetResources(ctx, &resourceservice.ResourceRequest{
		Kind:  "*",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("GetResources(*): %v", err)
	}

	kinds := map[string]bool{}
	for _, r := range resp.Resources {
		kinds[r.Kind] = true
	}
	for _, want := range []string{"HarvesterVM", "StoragePlatform", "NetworkIntegration"} {
		if !kinds[want] {
			t.Errorf("wildcard query missing kind %s; got kinds=%v", want, keys(kinds))
		}
	}
}

func TestE2E_GetResourceDetail_StoragePlatform(t *testing.T) {
	client, done := dial(t)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := client.GetResourceDetail(ctx, &resourceservice.ResourceDetailRequest{
		Kind:      "StoragePlatform",
		Name:      "e2e-storage",
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("GetResourceDetail: %v", err)
	}
	if resp.Name != "e2e-storage" {
		t.Errorf("name = %q, want e2e-storage", resp.Name)
	}
	if resp.Kind != "StoragePlatform" {
		t.Errorf("kind = %q, want StoragePlatform", resp.Kind)
	}
}

func names(rs []*resourceservice.ResourceStatus) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
