package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

var testListKinds = map[schema.GroupVersionResource]string{
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "harvestervms"}:         "HarvesterVMList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "storageplatforms"}:     "StoragePlatformList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "networkintegrations"}:  "NetworkIntegrationList",
}

func newTestServer(objects ...runtime.Object) *server {
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, testListKinds, objects...)
	return &server{
		dynamicClient: fakeClient,
		config:        defaultConfig(),
	}
}

// newTestServerWithMissingKind builds a server whose fake dynamic
// client knows the given GVR (so it doesn't panic on a List call)
// but a reactor intercepts that List and returns NotFound — the
// same response shape the real API server uses when a CRD is gone
// or a beta API version has been retired. The fake's "kind is
// registered with customListKinds" check runs *before* reactors,
// so the GVR has to be in the kind map for the reactor to even
// get a chance.
func newTestServerWithMissingKind(missing schema.GroupVersionResource, listKind string, objects ...runtime.Object) *server {
	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{}
	for k, v := range testListKinds {
		kinds[k] = v
	}
	kinds[missing] = listKind
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds, objects...)
	fakeClient.PrependReactor("list", missing.Resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: missing.Group, Resource: missing.Resource}, "")
	})
	return &server{dynamicClient: fakeClient, config: defaultConfig()}
}

func TestGetResources_SkipsKindsMissingFromCluster(t *testing.T) {
	vm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata":   map[string]any{"name": "vm-still-here", "namespace": "default"},
			"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				"vm":         map[string]any{"name": "vm-still-here"},
			},
		},
	}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})

	// One healthy kind from defaultConfig, plus an additional kind
	// the (simulated) cluster doesn't serve. With the resilience fix
	// the call should succeed and just drop the broken kind.
	missing := schema.GroupVersionResource{Group: "example.com", Version: "v1beta1", Resource: "gonefromclusters"}
	s := newTestServerWithMissingKind(missing, "GoneFromClusterList", vm)
	s.config.Resources["GoneFromCluster"] = ResourceKind{
		Group: missing.Group, Version: missing.Version, Resource: missing.Resource,
	}

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "*",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("expected nil error (broken kind should be skipped), got %v", err)
	}
	if len(resp.Resources) != 1 || resp.Resources[0].Name != "vm-still-here" {
		t.Fatalf("expected one resource from the healthy kind, got %+v", resp.Resources)
	}
	if resp.Resources[0].Kind != "HarvesterVM" {
		t.Errorf("expected Kind=HarvesterVM, got %q", resp.Resources[0].Kind)
	}
}

func TestGetResources_AllKindsMissingReturnsEmpty(t *testing.T) {
	// Edge case: every configured kind is missing → no error, empty
	// list. The dashboard shows the "No resources found" empty state
	// instead of an HTTP 500. Failing loudly here would only make
	// every fresh install with a stale config look broken.
	missing := schema.GroupVersionResource{Group: "example.com", Version: "v1beta1", Resource: "onlygonekinds"}
	s := newTestServerWithMissingKind(missing, "OnlyGoneKindList")
	s.config.Resources = map[string]ResourceKind{
		"OnlyGoneKind": {Group: missing.Group, Version: missing.Version, Resource: missing.Resource},
	}

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "*",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(resp.Resources) != 0 {
		t.Errorf("expected empty result, got %d resources", len(resp.Resources))
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
		Kind:  "HarvesterVM",
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
		Kind:  "HarvesterVM",
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
			"kind":       "HarvesterVM",
			"metadata": map[string]any{
				"name":      "test-vm",
				"namespace": "default",
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
				"vm": map[string]any{
					"name":  "my-vm",
					"ready": true,
				},
				"volume": map[string]any{
					"ready": true,
				},
				"cloudInit": map[string]any{
					"ready": true,
				},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "resources.stuttgart-things.com",
		Version: "v1alpha1",
		Kind:    "HarvesterVM",
	})

	s := newTestServer(obj)

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "HarvesterVM",
		Count: -1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resp.Resources))
	}

	r := resp.Resources[0]
	if r.Name != "test-vm" {
		t.Errorf("expected name test-vm, got %s", r.Name)
	}
	if r.Kind != "HarvesterVM" {
		t.Errorf("expected kind HarvesterVM, got %s", r.Kind)
	}
	if !r.Ready {
		t.Error("expected ready=true")
	}
	if r.StatusMessage != "Ready" {
		t.Errorf("expected status Ready, got %s", r.StatusMessage)
	}
	// ConnectionField is "status.vm.name" => "my-vm", plus StatusFields
	if !strings.Contains(r.ConnectionDetails, "my-vm") {
		t.Errorf("expected connection details to contain 'my-vm', got %s", r.ConnectionDetails)
	}
}

func TestGetResources_CountLimit(t *testing.T) {
	var objects []runtime.Object
	for i := 0; i < 5; i++ {
		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "resources.stuttgart-things.com/v1alpha1",
				"kind":       "HarvesterVM",
				"metadata": map[string]any{
					"name":      fmt.Sprintf("vm-%d", i),
					"namespace": "default",
				},
			},
		}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
		})
		objects = append(objects, obj)
	}

	s := newTestServer(objects...)

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "HarvesterVM",
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
		{
			name: "gateway api parent conditions all true",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"parents": []any{
						map[string]any{
							"conditions": []any{
								map[string]any{"type": "Accepted", "status": "True"},
								map[string]any{"type": "ResolvedRefs", "status": "True"},
							},
						},
					},
				},
			},
			wantMsg:   "Ready",
			wantReady: true,
		},
		{
			name: "gateway api parent condition not accepted",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"parents": []any{
						map[string]any{
							"conditions": []any{
								map[string]any{"type": "Accepted", "status": "False", "reason": "NoMatchingParent"},
								map[string]any{"type": "ResolvedRefs", "status": "True"},
							},
						},
					},
				},
			},
			wantMsg:   "Accepted: NoMatchingParent",
			wantReady: false,
		},
		{
			name: "gateway api parent without conditions",
			obj: map[string]any{
				"metadata": map[string]any{"name": "test"},
				"status": map[string]any{
					"parents": []any{
						map[string]any{"parentRef": map[string]any{"name": "gw"}},
					},
				},
			},
			wantMsg:   "No conditions found",
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

func TestGetConnectionDetails(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "test"},
		"status": map[string]any{
			"share": map[string]any{"ips": "10.0.0.1"},
			"vm":    map[string]any{"name": "my-vm"},
		},
	}}

	tests := []struct {
		name      string
		fieldPath string
		want      string
	}{
		{"nested string", "status.share.ips", "10.0.0.1"},
		{"vm name", "status.vm.name", "my-vm"},
		{"empty path", "", ""},
		{"missing field", "status.nonexistent.field", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getConnectionDetails(obj, tt.fieldPath)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGetStatusDetails(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "test"},
		"status": map[string]any{
			"installed":       true,
			"observedVersion": "1.2.3",
			"volume":          map[string]any{"ready": true},
		},
	}}

	tests := []struct {
		name   string
		fields []string
		want   string
	}{
		{"bool and string fields", []string{"status.installed", "status.observedVersion"}, "installed=true, observedVersion=1.2.3"},
		{"nested bool", []string{"status.volume.ready"}, "ready=true"},
		{"missing fields", []string{"status.nonexistent"}, ""},
		{"empty fields", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStatusDetails(obj, tt.fields)
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGetNestedField(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "test"},
		"status": map[string]any{
			"name":   "my-resource",
			"ready":  true,
			"count":  int64(42),
			"nested": map[string]any{"deep": "value"},
		},
		"spec": map[string]any{
			"hostnames": []any{"a.example.com", "b.example.com"},
			"parentRefs": []any{
				map[string]any{"name": "gateway-a", "namespace": "default"},
				map[string]any{"name": "gateway-b"},
			},
		},
	}}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"string field", "status.name", "my-resource"},
		{"bool field", "status.ready", "true"},
		{"int64 field", "status.count", "42"},
		{"nested field", "status.nested.deep", "value"},
		{"missing field", "status.missing", ""},
		{"slice of strings (hostnames)", "spec.hostnames", "a.example.com, b.example.com"},
		{"slice of maps (parentRefs)", "spec.parentRefs", "default/gateway-a, gateway-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getNestedField(obj, tt.path)
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
