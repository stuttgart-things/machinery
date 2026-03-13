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
	srv := newTestServer()
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
	if !strings.Contains(body, "machinery") {
		t.Error("expected 'machinery' in response body")
	}
	if !strings.Contains(body, "htmx") {
		t.Error("expected 'htmx' in response body")
	}
}

func TestWebResources(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "AnsibleRun",
			"metadata": map[string]any{
				"name":      "web-test-run",
				"namespace": "default",
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True"},
				},
				"share": map[string]any{"ips": "192.168.1.1"},
			},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "AnsibleRun",
	})

	srv := newTestServer(obj)
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/resources?kind=AnsibleRun", nil)
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "web-test-run") {
		t.Error("expected resource name in response")
	}
	if !strings.Contains(body, "192.168.1.1") {
		t.Error("expected IP in response")
	}
	if !strings.Contains(body, "Ready") {
		t.Error("expected Ready badge in response")
	}
}

func TestWebResourcesEmpty(t *testing.T) {
	srv := newTestServer()
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	req := httptest.NewRequest("GET", "/resources?kind=AnsibleRun", nil)
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
	srv := newTestServer()
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
	srv := newTestServer()
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

// Ensure unused import is used
var _ runtime.Object
