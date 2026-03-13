package main

import (
	"context"
	"fmt"
	"testing"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var testListKinds = map[schema.GroupVersionResource]string{
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "ansibleruns"}:       "AnsibleRunList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "vspherevmansibles"}: "VsphereVMAnsibleList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "proxmoxvmansibles"}: "ProxmoxVMAnsibleList",
}

func newTestServer(objects ...runtime.Object) *server {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, testListKinds, objects...)
	return &server{
		dynamicClient: fakeClient,
		config:        defaultConfig(),
	}
}

func TestGetResources_InvalidKind(t *testing.T) {
	s := newTestServer()
	_, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "NonExistent",
		Count: 5,
	})
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestGetResources_InvalidCount(t *testing.T) {
	s := newTestServer()
	_, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "AnsibleRun",
		Count: 5000,
	})
	if err == nil {
		t.Fatal("expected error for count > 1000")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestGetResources_EmptyResult(t *testing.T) {
	s := newTestServer()
	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "AnsibleRun",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resp.Resources))
	}
}

func TestGetResources_WithResources(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "AnsibleRun",
			"metadata": map[string]any{
				"name":      "test-run",
				"namespace": "default",
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
				"share": map[string]any{
					"ips": "10.0.0.1",
				},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "resources.stuttgart-things.com",
		Version: "v1alpha1",
		Kind:    "AnsibleRun",
	})

	s := newTestServer(obj)

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "AnsibleRun",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
	}

	r := resp.Resources[0]
	if r.Name != "test-run" {
		t.Errorf("expected name test-run, got %s", r.Name)
	}
	if r.Kind != "AnsibleRun" {
		t.Errorf("expected kind AnsibleRun, got %s", r.Kind)
	}
	if !r.Ready {
		t.Error("expected ready=true")
	}
	if r.StatusMessage != "Ready" {
		t.Errorf("expected status Ready, got %s", r.StatusMessage)
	}
	if r.ConnectionDetails != "10.0.0.1" {
		t.Errorf("expected IPs 10.0.0.1, got %s", r.ConnectionDetails)
	}
}

func TestGetResources_CountLimit(t *testing.T) {
	var objects []runtime.Object
	for i := 0; i < 5; i++ {
		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "resources.stuttgart-things.com/v1alpha1",
				"kind":       "AnsibleRun",
				"metadata": map[string]any{
					"name":      fmt.Sprintf("run-%d", i),
					"namespace": "default",
				},
			},
		}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "AnsibleRun",
		})
		objects = append(objects, obj)
	}

	s := newTestServer(objects...)

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "AnsibleRun",
		Count: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Resources) != 2 {
		t.Errorf("expected 2 resources (count limit), got %d", len(resp.Resources))
	}
}

func TestGetResourceStatus(t *testing.T) {
	tests := []struct {
		name       string
		obj        map[string]any
		wantMsg    string
		wantReady  bool
	}{
		{
			name:      "ready condition",
			obj:       makeObj("Ready", "True"),
			wantMsg:   "Ready",
			wantReady: true,
		},
		{
			name:      "not ready condition",
			obj:       makeObj("Ready", "False"),
			wantMsg:   "Not Ready",
			wantReady: false,
		},
		{
			name: "no conditions",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
			},
			wantMsg:   "No conditions found",
			wantReady: false,
		},
		{
			name: "no ready type condition",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"conditions": []any{
						map[string]any{"type": "Available", "status": "True"},
					},
				},
			},
			wantMsg:   "Not Ready",
			wantReady: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: tt.obj}
			msg, ready := getResourceStatus(obj)
			if msg != tt.wantMsg {
				t.Errorf("expected message %q, got %q", tt.wantMsg, msg)
			}
			if ready != tt.wantReady {
				t.Errorf("expected ready=%v, got %v", tt.wantReady, ready)
			}
		})
	}
}

func TestGetIps(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{
			name: "has IPs",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"share": map[string]any{"ips": "10.0.0.1"},
				},
			},
			want: "10.0.0.1",
		},
		{
			name: "no share",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
			},
			want: "NO STATUS FOUND",
		},
		{
			name: "share without IPs",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"share": map[string]any{"other": "value"},
				},
			},
			want: "NO IPS FOUND IN STATUS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: tt.obj}
			got := getIps(obj)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func makeObj(condType, condStatus string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": "test"},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{
					"type":   condType,
					"status": condStatus,
				},
			},
		},
	}
}
