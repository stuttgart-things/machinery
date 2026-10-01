package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	resourceservice "github.com/stuttgart-things/machinery/resourceservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

var testListKinds = map[schema.GroupVersionResource]string{
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "harvestervms"}:        "HarvesterVMList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "storageplatforms"}:    "StoragePlatformList",
	{Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "networkintegrations"}: "NetworkIntegrationList",
}

func newTestServer(t *testing.T, objects ...runtime.Object) *server {
	t.Helper()
	scheme := runtime.NewScheme()
	fakeClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, testListKinds, objects...)
	return buildTestServer(t, fakeClient, defaultConfig())
}

// buildTestServer wires the production startInformers() against a fake
// dynamic client, so tests exercise the same informer-cache read path
// as the running server. The informers are torn down via t.Cleanup.
func buildTestServer(t *testing.T, dc *dynamicfake.FakeDynamicClient, cfg *Config) *server {
	t.Helper()
	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })
	infs, _, _ := startInformers(dc, cfg, stopCh)
	return &server{config: cfg, informers: infs}
}

// fakeClientWithMissingKind builds a fake dynamic client that knows the
// given GVR's list-kind (so List doesn't panic) but whose List reactor
// returns NotFound — the same response shape the real API server uses
// when a CRD is gone or a beta API version has been retired. The fake's
// "kind is registered with customListKinds" check runs *before*
// reactors, so the GVR has to be in the kind map for the reactor to get
// a chance. startInformers' startup probe sees the NotFound and skips
// the kind, attaching no informer for it.
func fakeClientWithMissingKind(missing schema.GroupVersionResource, listKind string, objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
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
	return fakeClient
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

	// defaultConfig's healthy kinds, plus one the (simulated) cluster
	// doesn't serve. The missing kind must be in the config *before*
	// the server is built, so startInformers' startup probe is what
	// drops it — the call should then succeed and just omit that kind.
	missing := schema.GroupVersionResource{Group: "example.com", Version: "v1beta1", Resource: "gonefromclusters"}
	cfg := defaultConfig()
	cfg.Resources["GoneFromCluster"] = ResourceKind{
		Group: missing.Group, Version: missing.Version, Resource: missing.Resource,
	}
	s := buildTestServer(t, fakeClientWithMissingKind(missing, "GoneFromClusterList", vm), cfg)

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
	cfg := &Config{
		Port:     50051,
		HttpPort: 8080,
		Resources: map[string]ResourceKind{
			"OnlyGoneKind": {Group: missing.Group, Version: missing.Version, Resource: missing.Resource},
		},
	}
	s := buildTestServer(t, fakeClientWithMissingKind(missing, "OnlyGoneKindList"), cfg)

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
	s := newTestServer(t)
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
	s := newTestServer(t)
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
	s := newTestServer(t)
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

	s := newTestServer(t, obj)

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

	s := newTestServer(t, objects...)

	resp, err := s.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  "HarvesterVM",
		Count: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Resources) != 2 {
		t.Fatalf("expected 2 resources (count limit), got %d", len(resp.Resources))
	}
	// The cap is applied after the sort, so it is a deterministic
	// top-N — the first two by (kind, namespace, name), not an
	// arbitrary subset of the informer store's iteration order.
	if resp.Resources[0].Name != "vm-0" || resp.Resources[1].Name != "vm-1" {
		t.Errorf("expected the sorted top-2 [vm-0 vm-1], got [%s %s]",
			resp.Resources[0].Name, resp.Resources[1].Name)
	}
}

func TestGetResourceDetail(t *testing.T) {
	vm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata":   map[string]any{"name": "detail-vm", "namespace": "team-x"},
			"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				"vm":         map[string]any{"name": "the-vm"},
			},
		},
	}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})
	s := newTestServer(t, vm)

	t.Run("found, served from cache", func(t *testing.T) {
		resp, err := s.GetResourceDetail(context.Background(), &resourceservice.ResourceDetailRequest{
			Kind: "HarvesterVM", Name: "detail-vm", Namespace: "team-x",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Name != "detail-vm" || !resp.Ready {
			t.Errorf("unexpected detail: %+v", resp)
		}
		if !strings.Contains(resp.ConnectionDetails, "the-vm") {
			t.Errorf("expected connection details to contain 'the-vm', got %q", resp.ConnectionDetails)
		}
	})

	t.Run("missing resource returns NotFound", func(t *testing.T) {
		_, err := s.GetResourceDetail(context.Background(), &resourceservice.ResourceDetailRequest{
			Kind: "HarvesterVM", Name: "nope", Namespace: "team-x",
		})
		if st, _ := status.FromError(err); st.Code() != codes.NotFound {
			t.Errorf("expected NotFound, got %v", st.Code())
		}
	})

	t.Run("unsupported kind returns InvalidArgument", func(t *testing.T) {
		_, err := s.GetResourceDetail(context.Background(), &resourceservice.ResourceDetailRequest{
			Kind: "Nonexistent", Name: "x", Namespace: "y",
		})
		if st, _ := status.FromError(err); st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", st.Code())
		}
	})
}

func TestGetResourceStatus(t *testing.T) {
	tests := []struct {
		name      string
		obj       map[string]any
		wantMsg   string
		wantReady bool
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

// --- WatchResources ---

// mockWatchStream is a minimal ResourceService_WatchResourcesServer: it
// records Send()s on a channel and returns a controllable context.
// WatchResources only calls Context and Send, so the embedded (nil)
// ServerStream is never dereferenced.
type mockWatchStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan *resourceservice.ResourceEvent
}

func (m *mockWatchStream) Context() context.Context { return m.ctx }

func (m *mockWatchStream) Send(e *resourceservice.ResourceEvent) error {
	m.sent <- e
	return nil
}

func harvesterVM(name, ns string) *unstructured.Unstructured {
	vm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata":   map[string]any{"name": name, "namespace": ns},
			"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				"vm":         map[string]any{"name": name},
			},
		},
	}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})
	return vm
}

func TestWatchResources_InitialSnapshotThenCancel(t *testing.T) {
	s := newTestServer(t, harvesterVM("watch-vm", "ns-1"))

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockWatchStream{ctx: ctx, sent: make(chan *resourceservice.ResourceEvent, 16)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.WatchResources(&resourceservice.ResourceRequest{Kind: "HarvesterVM"}, stream)
	}()

	// The informer replays its cache to a freshly-attached handler, so
	// the already-cached object arrives as an ADDED event.
	select {
	case ev := <-stream.sent:
		if ev.Type != resourceservice.EventType_ADDED {
			t.Errorf("expected ADDED, got %v", ev.Type)
		}
		if ev.Resource.GetName() != "watch-vm" {
			t.Errorf("expected watch-vm, got %q", ev.Resource.GetName())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ADDED event for the cached object")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("WatchResources returned %v, want nil on client cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WatchResources did not return after the client cancelled")
	}
}

func TestWatchResources_InvalidKind(t *testing.T) {
	s := newTestServer(t)
	stream := &mockWatchStream{ctx: context.Background(), sent: make(chan *resourceservice.ResourceEvent, 1)}
	err := s.WatchResources(&resourceservice.ResourceRequest{Kind: "Nonexistent"}, stream)
	if st, _ := status.FromError(err); st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v (err=%v)", st.Code(), err)
	}
}

func TestWatchResources_LiveEvents(t *testing.T) {
	scheme := runtime.NewScheme()
	fc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, testListKinds)
	s := buildTestServer(t, fc, defaultConfig())

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockWatchStream{ctx: ctx, sent: make(chan *resourceservice.ResourceEvent, 16)}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.WatchResources(&resourceservice.ResourceRequest{Kind: "HarvesterVM"}, stream)
	}()

	// Create an object once the watch is live; the informer observes it
	// and the handler emits an ADDED event.
	gvr := schema.GroupVersionResource{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "harvestervms",
	}
	if _, err := fc.Resource(gvr).Namespace("ns-live").Create(
		context.Background(), harvesterVM("live-vm", "ns-live"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}

	select {
	case ev := <-stream.sent:
		if ev.Resource.GetName() != "live-vm" {
			t.Errorf("expected live-vm, got %q", ev.Resource.GetName())
		}
		if ev.Type != resourceservice.EventType_ADDED {
			t.Errorf("expected ADDED, got %v", ev.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event for the created object")
	}

	cancel()
	<-errCh
}

// TestRetryUnservedKinds_AttachesLateKind is the regression test for
// stuttgart-things/clusterbook#186: a kind whose CRD does not exist when
// the process starts used to be dropped for the lifetime of the pod, so
// a dashboard on a freshly built cluster stayed empty until somebody
// restarted it by hand.
//
// The fake client returns NotFound for the kind until the reactor is
// flipped, which is exactly what the API server does between pod start
// and Crossplane establishing the XRD.
func TestRetryUnservedKinds_AttachesLateKind(t *testing.T) {
	late := schema.GroupVersionResource{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Resource: "latevms",
	}

	var served atomic.Bool
	scheme := runtime.NewScheme()
	kinds := map[schema.GroupVersionResource]string{}
	for k, v := range testListKinds {
		kinds[k] = v
	}
	kinds[late] = "LateVMList"
	dc := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, kinds)
	dc.PrependReactor("list", late.Resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
		if served.Load() {
			return false, nil, nil // fall through to the tracker
		}
		return true, nil, apierrors.NewNotFound(
			schema.GroupResource{Group: late.Group, Resource: late.Resource}, "")
	})

	cfg := defaultConfig()
	cfg.Resources = map[string]ResourceKind{
		"LateVM": {Group: late.Group, Version: late.Version, Resource: late.Resource},
	}

	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })

	infs, factory, unserved := startInformers(dc, cfg, stopCh)
	if len(infs) != 0 {
		t.Fatalf("expected no informer while the kind is NotFound, got %d", len(infs))
	}
	if len(unserved) != 1 || unserved[0] != "LateVM" {
		t.Fatalf("expected LateVM to be reported unserved, got %v", unserved)
	}

	srv := &server{config: cfg, informers: infs}
	if _, ok := srv.informer("LateVM"); ok {
		t.Fatal("informer present before the kind is served")
	}

	// Shorten the retry so the test does not wait 30s for a tick.
	orig := informerRetryInterval
	informerRetryInterval = 10 * time.Millisecond
	t.Cleanup(func() { informerRetryInterval = orig })

	go retryUnservedKinds(dc, srv, factory, unserved, stopCh)

	// The CRD "appears".
	served.Store(true)

	deadline := time.After(10 * time.Second)
	for {
		if _, ok := srv.informer("LateVM"); ok {
			return // recovered without a restart — the point of the fix
		}
		select {
		case <-deadline:
			t.Fatal("informer for LateVM never attached after the kind became served")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// --- ResourceStatus: conditions, generation, creationTimestamp (#96) ---

func TestToResourceStatus_Conditions(t *testing.T) {
	rk := ResourceKind{}

	t.Run("with conditions, order and fields preserved", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{
				"name":              "c1",
				"generation":        int64(4),
				"creationTimestamp": "2026-09-30T10:00:00Z",
			},
			"status": map[string]any{
				"observedGeneration": int64(3),
				"conditions": []any{
					map[string]any{
						"type": "Synced", "status": "True", "reason": "ReconcileSuccess",
						"lastTransitionTime": "2026-09-30T10:01:00Z",
					},
					map[string]any{
						"type": "Ready", "status": "False", "reason": "Creating",
						"message": "PipelineRun failed", "lastTransitionTime": "2026-09-30T10:02:00Z",
					},
				},
			},
		}}
		rs := toResourceStatus(obj, "ClusterStack", rk)
		if rs.Ready || rs.StatusMessage != "Not Ready" {
			t.Errorf("ready/status_message changed: ready=%v msg=%q", rs.Ready, rs.StatusMessage)
		}
		if len(rs.Conditions) != 2 {
			t.Fatalf("expected 2 conditions, got %d", len(rs.Conditions))
		}
		c0, c1 := rs.Conditions[0], rs.Conditions[1]
		if c0.Type != "Synced" || c0.Status != "True" || c0.Reason != "ReconcileSuccess" ||
			c0.Message != "" || c0.LastTransitionTime != "2026-09-30T10:01:00Z" {
			t.Errorf("unexpected condition[0]: %+v", c0)
		}
		if c1.Type != "Ready" || c1.Status != "False" || c1.Reason != "Creating" ||
			c1.Message != "PipelineRun failed" || c1.LastTransitionTime != "2026-09-30T10:02:00Z" {
			t.Errorf("unexpected condition[1]: %+v", c1)
		}
		if rs.Generation != 4 || rs.ObservedGeneration != 3 {
			t.Errorf("generation=%d observed=%d, want 4/3", rs.Generation, rs.ObservedGeneration)
		}
		if rs.CreationTimestamp != "2026-09-30T10:00:00Z" {
			t.Errorf("creation_timestamp=%q", rs.CreationTimestamp)
		}
	})

	t.Run("without status", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "c2"},
		}}
		rs := toResourceStatus(obj, "X", rk)
		if len(rs.Conditions) != 0 || rs.Generation != 0 || rs.ObservedGeneration != 0 || rs.CreationTimestamp != "" {
			t.Errorf("expected zero values, got %+v", rs)
		}
		if rs.StatusMessage != "No conditions found" || rs.Ready {
			t.Errorf("ready/status_message changed: %v %q", rs.Ready, rs.StatusMessage)
		}
	})

	t.Run("empty conditions list", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "c3"},
			"status":   map[string]any{"conditions": []any{}},
		}}
		rs := toResourceStatus(obj, "X", rk)
		if len(rs.Conditions) != 0 {
			t.Errorf("expected no conditions, got %+v", rs.Conditions)
		}
		if rs.StatusMessage != "Not Ready" || rs.Ready {
			t.Errorf("ready/status_message changed: %v %q", rs.Ready, rs.StatusMessage)
		}
	})

	t.Run("missing observedGeneration is 0", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "c4", "generation": int64(7)},
			"status": map[string]any{
				"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
			},
		}}
		rs := toResourceStatus(obj, "X", rk)
		if rs.Generation != 7 || rs.ObservedGeneration != 0 {
			t.Errorf("generation=%d observed=%d, want 7/0", rs.Generation, rs.ObservedGeneration)
		}
		if !rs.Ready || rs.StatusMessage != "Ready" {
			t.Errorf("ready/status_message changed: %v %q", rs.Ready, rs.StatusMessage)
		}
	})

	t.Run("long message is truncated", func(t *testing.T) {
		long := strings.Repeat("ä", maxConditionMessageBytes) // 2 bytes per rune
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "c5"},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "False", "message": long},
					map[string]any{"type": "Synced", "status": "True", "message": "short"},
				},
			},
		}}
		rs := toResourceStatus(obj, "X", rk)
		msg := rs.Conditions[0].Message
		if len(msg) > maxConditionMessageBytes {
			t.Errorf("message is %d bytes, cap is %d", len(msg), maxConditionMessageBytes)
		}
		if !strings.HasSuffix(msg, conditionTruncationMarker) {
			t.Errorf("truncated message lacks marker")
		}
		if !utf8.ValidString(msg) {
			t.Error("truncated message is not valid UTF-8")
		}
		if rs.Conditions[1].Message != "short" {
			t.Errorf("short message changed: %q", rs.Conditions[1].Message)
		}
	})

	t.Run("malformed entries are skipped or ignored", func(t *testing.T) {
		// Only JSON-decodable value types (int64, float64, bool, nil,
		// string, map, slice): that is all an informer can hand us, and
		// the existing getResourceStatus deep-copies via NestedSlice,
		// which rejects plain Go int.
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{
				"name":              "c6",
				"generation":        "not-a-number",
				"creationTimestamp": int64(12345),
			},
			"status": map[string]any{
				"observedGeneration": []any{int64(1)},
				"conditions": []any{
					"just a string",
					nil,
					int64(42),
					map[string]any{"type": int64(1), "status": true, "reason": nil, "message": map[string]any{}, "lastTransitionTime": float64(99)},
					map[string]any{"type": "Ready", "status": "True"},
				},
			},
		}}
		rs := toResourceStatus(obj, "X", rk)
		if len(rs.Conditions) != 2 {
			t.Fatalf("expected 2 conditions (non-maps skipped), got %d: %+v", len(rs.Conditions), rs.Conditions)
		}
		if c := rs.Conditions[0]; c.Type != "" || c.Status != "" || c.Reason != "" || c.Message != "" || c.LastTransitionTime != "" {
			t.Errorf("non-string fields should be empty, got %+v", c)
		}
		if rs.Conditions[1].Type != "Ready" {
			t.Errorf("unexpected condition[1]: %+v", rs.Conditions[1])
		}
		if rs.Generation != 0 || rs.ObservedGeneration != 0 || rs.CreationTimestamp != "" {
			t.Errorf("malformed metadata should yield zero values, got gen=%d obs=%d ts=%q",
				rs.Generation, rs.ObservedGeneration, rs.CreationTimestamp)
		}
		if !rs.Ready {
			t.Error("ready should still be derived from the Ready condition")
		}
	})

	t.Run("conditions not a list", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "c7"},
			"status":   map[string]any{"conditions": "oops"},
		}}
		rs := toResourceStatus(obj, "X", rk)
		if len(rs.Conditions) != 0 {
			t.Errorf("expected no conditions, got %+v", rs.Conditions)
		}
	})

	t.Run("gateway kind: parents aggregated, conditions empty", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": "route"},
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
		}}
		rs := toResourceStatus(obj, "HTTPRoute", rk)
		if rs.Ready || rs.StatusMessage != "Accepted: NoMatchingParent" {
			t.Errorf("gateway aggregation changed: ready=%v msg=%q", rs.Ready, rs.StatusMessage)
		}
		if len(rs.Conditions) != 0 {
			t.Errorf("expected no conditions for parents-only status, got %+v", rs.Conditions)
		}
	})
}

func TestTruncateMessage(t *testing.T) {
	if got := truncateMessage("abc", 3); got != "abc" {
		t.Errorf("at cap: got %q", got)
	}
	if got := truncateMessage("abcdef", 5); got != "ab"+conditionTruncationMarker {
		t.Errorf("over cap: got %q", got)
	}
	if got := truncateMessage("abcdef", 1); got != conditionTruncationMarker {
		t.Errorf("cap smaller than marker: got %q", got)
	}
}

func TestGetResourceDetail_CarriesConditions(t *testing.T) {
	vm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata": map[string]any{
				"name": "cond-vm", "namespace": "team-x", "generation": int64(2),
			},
			"status": map[string]any{
				"observedGeneration": int64(2),
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "False", "reason": "Creating", "message": "waiting"},
				},
			},
		},
	}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})
	s := newTestServer(t, vm)
	resp, err := s.GetResourceDetail(context.Background(), &resourceservice.ResourceDetailRequest{
		Kind: "HarvesterVM", Name: "cond-vm", Namespace: "team-x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Conditions) != 1 || resp.Conditions[0].Reason != "Creating" || resp.Conditions[0].Message != "waiting" {
		t.Errorf("unexpected conditions: %+v", resp.Conditions)
	}
	if resp.Generation != 2 || resp.ObservedGeneration != 2 {
		t.Errorf("generation=%d observed=%d, want 2/2", resp.Generation, resp.ObservedGeneration)
	}
}
