package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestWebIndex(t *testing.T) {
	srv := newTestServer(t)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"machinery", "htmx",
		// Multi-select filter scaffolding — the JS owns state, the
		// chips just need the right hooks. If any of these go missing
		// the click handler stops binding and filtering breaks
		// silently in the browser.
		`id="kind-filters"`,
		`id="namespace-filters"`,
		`data-value="*"`,
		// Stale-response guard for the auto-refresh poll.
		`hx-sync="this:abort"`,
		`filterChange`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in index body", want)
		}
	}
}

func TestWebResources(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata": map[string]any{
				"name":      "web-test-vm",
				"namespace": "default",
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True"},
				},
				"vm": map[string]any{"name": "my-vm", "ready": true},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})

	srv := newTestServer(t, obj)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "web-test-vm") {
		t.Error("expected resource name in response")
	}
	if !strings.Contains(body, "my-vm") {
		t.Error("expected vm name in response")
	}
	if !strings.Contains(body, "Ready") {
		t.Error("expected Ready badge in response")
	}
}

func TestWebResourcesEmpty(t *testing.T) {
	srv := newTestServer(t)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No resources found") {
		t.Error("expected empty state message")
	}
}

func TestWebHealth(t *testing.T) {
	srv := newTestServer(t)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("expected 'ok', got %q", rr.Body.String())
	}
}

func TestWebNotFound(t *testing.T) {
	srv := newTestServer(t)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestWebResourcesNamespaceFilter(t *testing.T) {
	mkVM := func(name, ns string) runtime.Object {
		obj := &unstructured.Unstructured{
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
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
		})
		return obj
	}

	srv := newTestServer(t, mkVM("vm-a", "team-a"), mkVM("vm-b", "team-b"), mkVM("vm-c", "team-a"))
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	t.Run("no filter returns all", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM", nil)
		rr := httptest.NewRecorder()
		webSrv.handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		for _, name := range []string{"vm-a", "vm-b", "vm-c"} {
			if !strings.Contains(body, name) {
				t.Errorf("expected %s in response", name)
			}
		}
	})

	t.Run("single namespace narrows result", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM&namespace=team-a", nil)
		rr := httptest.NewRecorder()
		webSrv.handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		if !strings.Contains(body, "vm-a") || !strings.Contains(body, "vm-c") {
			t.Error("expected vm-a and vm-c (both in team-a)")
		}
		if strings.Contains(body, "vm-b") {
			t.Error("did not expect vm-b (in team-b)")
		}
	})

	t.Run("multi-namespace via CSV", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM&namespace=team-a,team-b", nil)
		rr := httptest.NewRecorder()
		webSrv.handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		for _, name := range []string{"vm-a", "vm-b", "vm-c"} {
			if !strings.Contains(body, name) {
				t.Errorf("expected %s in response", name)
			}
		}
	})

	t.Run("ns-data metadata lists both namespaces sorted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM&namespace=team-a", nil)
		rr := httptest.NewRecorder()
		webSrv.handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		// The metadata div must enumerate ALL namespaces in the kind-
		// filtered set, not just the ones surviving the namespace
		// filter — otherwise the chip row collapses to the active set
		// and users can't deselect.
		if !strings.Contains(body, `data-namespaces='["team-a","team-b"]'`) {
			t.Errorf("expected pre-filter ns metadata in response, got body:\n%s", body)
		}
	})

	t.Run("cluster-scoped resources bypass namespace filter", func(t *testing.T) {
		clusterObj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "resources.stuttgart-things.com/v1alpha1",
				"kind":       "HarvesterVM",
				"metadata":   map[string]any{"name": "cluster-scoped"},
				"status": map[string]any{
					"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				},
			},
		}
		clusterObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
		})
		srv2 := newTestServer(t, mkVM("vm-a", "team-a"), clusterObj)
		webSrv2, _ := newWebServer(srv2)

		req := httptest.NewRequest("GET", "/resources?kind=HarvesterVM&namespace=team-a", nil)
		rr := httptest.NewRecorder()
		webSrv2.handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		if !strings.Contains(body, "cluster-scoped") {
			t.Error("expected cluster-scoped resource to bypass the namespace filter")
		}
	})
}

// Ensure unused import is used
var _ runtime.Object
