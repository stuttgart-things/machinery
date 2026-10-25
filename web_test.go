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
	webSrv, err := newWebServer(srv, "")
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
		// Pause / manual-refresh controls (#75).
		`id="toggle-refresh"`,
		`id="refresh-now"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in index body", want)
		}
	}
	// #75: the declarative htmx poller was replaced by a JS interval so
	// every tick carries the live filter — the fixed-URL `every 5s`
	// trigger must be gone.
	if strings.Contains(body, "every 5s") {
		t.Error("index still has the htmx `every 5s` poller; expected JS-driven refresh")
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
	webSrv, err := newWebServer(srv, "")
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
	webSrv, err := newWebServer(srv, "")
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
	webSrv, err := newWebServer(srv, "")
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
	webSrv, err := newWebServer(srv, "")
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
	webSrv, err := newWebServer(srv, "")
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
		webSrv2, _ := newWebServer(srv2, "")

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

func TestParseBasePath(t *testing.T) {
	for in, want := range map[string]string{
		"":                "",
		"/":               "",
		"/machinery":      "/machinery",
		"/machinery/":     "/machinery",
		" /lab/machinery": "/lab/machinery",
	} {
		got, err := parseBasePath(in)
		if err != nil || got != want {
			t.Errorf("parseBasePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"machinery", "/a//b", "/a/../b", "/a b", "/a?x", "/a#x", "/a%2f"} {
		if got, err := parseBasePath(in); err == nil {
			t.Errorf("parseBasePath(%q) = %q, want an error", in, got)
		}
	}
}

// TestWebBasePath covers the sthings-portal prefix contract: the portal
// forwards /machinery/... unchanged, so every page answers under the prefix
// and every URL handed to the browser carries it. /health and the JSON API
// stay at the root too.
func TestWebBasePath(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "resources.stuttgart-things.com/v1alpha1",
			"kind":       "HarvesterVM",
			"metadata":   map[string]any{"name": "prefixed-vm", "namespace": "default"},
		},
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})
	webSrv, err := newWebServer(newTestServer(t, obj), "/machinery")
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}
	h := webSrv.handler()
	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		return rr
	}

	for _, path := range []string{
		"/machinery/",
		"/machinery/resources?kind=HarvesterVM",
		"/machinery/detail?kind=HarvesterVM&name=prefixed-vm&namespace=default",
		"/machinery/health",
		"/machinery/api/v1/resources",
		"/health",
		"/api/v1/resources",
	} {
		if rr := get(path); rr.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, rr.Code)
		}
	}
	for _, path := range []string{"/", "/resources?kind=*", "/detail?kind=HarvesterVM&name=prefixed-vm"} {
		if rr := get(path); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: expected 404 outside the prefix, got %d", path, rr.Code)
		}
	}
	if rr := get("/machinery"); rr.Code/100 != 3 || rr.Header().Get("Location") != "/machinery/" {
		t.Errorf("GET /machinery: expected redirect to /machinery/, got %d %q", rr.Code, rr.Header().Get("Location"))
	}

	index := get("/machinery/").Body.String()
	if !strings.Contains(index, `const BASE_PATH = "/machinery";`) {
		t.Error("index: expected the JS refresh URL to carry the prefix")
	}
	for path, body := range map[string]string{
		"index":     index,
		"resources": get("/machinery/resources?kind=HarvesterVM").Body.String(),
		"detail":    get("/machinery/detail?kind=HarvesterVM&name=prefixed-vm&namespace=default").Body.String(),
	} {
		n := strings.Count(body, `hx-get="`)
		if n == 0 {
			t.Errorf("%s: expected hx-get links", path)
		}
		if p := strings.Count(body, `hx-get="/machinery/`); p != n {
			t.Errorf("%s: %d of %d hx-get links lack the /machinery prefix", path, n-p, n)
		}
	}
}

// TestWebPortalEmbedding covers the sthings-portal embedded-header contract:
// the embedded class is set before the first paint, and the portal's design
// tokens are linked only for requests that came through the portal.
func TestWebPortalEmbedding(t *testing.T) {
	webSrv, err := newWebServer(newTestServer(t), "/machinery")
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}
	get := func(header ...string) string {
		req := httptest.NewRequest("GET", "/machinery/", nil)
		if len(header) == 2 {
			req.Header.Set(header[0], header[1])
		}
		rr := httptest.NewRecorder()
		webSrv.handler().ServeHTTP(rr, req)
		return rr.Body.String()
	}

	plain := get()
	for _, want := range []string{`classList.add('hr-embedded')`, `class="embedded-sub"`, ":where(:root)"} {
		if !strings.Contains(plain, want) {
			t.Errorf("index: expected %q", want)
		}
	}
	if strings.Contains(plain, "homerun2-tokens.css") {
		t.Error("index: links the portal tokens without X-Forwarded-Prefix")
	}
	if !strings.Contains(get("X-Forwarded-Prefix", "/machinery"), `<link rel="stylesheet" href="/static/homerun2-tokens.css">`) {
		t.Error("index through the portal: expected the token stylesheet")
	}
}
