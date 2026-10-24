package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func apiTestVM(name, ns string, annotations map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"name": name, "namespace": ns}
	if annotations != nil {
		meta["annotations"] = annotations
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "resources.stuttgart-things.com/v1alpha1",
		"kind":       "HarvesterVM",
		"metadata":   meta,
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "resources.stuttgart-things.com", Version: "v1alpha1", Kind: "HarvesterVM",
	})
	return obj
}

func getAPI(t *testing.T, srv *server, url string) (*httptest.ResponseRecorder, apiResourceList) {
	t.Helper()
	webSrv, err := newWebServer(srv)
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}
	rr := httptest.NewRecorder()
	webSrv.handler().ServeHTTP(rr, httptest.NewRequest("GET", url, nil))
	var list apiResourceList
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
			t.Fatalf("response is not JSON: %v\n%s", err, rr.Body.String())
		}
	}
	return rr, list
}

func TestAPIResourcesReturnsFleetAnnotationsOnly(t *testing.T) {
	srv := newTestServer(t, apiTestVM("vm-a", "default", map[string]any{
		"stuttgart-things.com/source-path":                 "clusters/labda/vsphere/machinery-xrs/vm-a.yaml",
		"stuttgart-things.com/management-cluster":          "machinery",
		"kubectl.kubernetes.io/last-applied-configuration": `{"spec":{"secret":"x"}}`,
	}))

	rr, list := getAPI(t, srv, "/api/v1/resources?kind=HarvesterVM")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list.Items))
	}
	got := list.Items[0]
	if got.Name != "vm-a" || got.Kind != "HarvesterVM" || got.Namespace != "default" || !got.Ready {
		t.Errorf("unexpected item: %+v", got)
	}
	if got.APIVersion != "resources.stuttgart-things.com/v1alpha1" {
		t.Errorf("apiVersion = %q", got.APIVersion)
	}
	if got.Annotations["stuttgart-things.com/source-path"] != "clusters/labda/vsphere/machinery-xrs/vm-a.yaml" {
		t.Errorf("source-path missing: %v", got.Annotations)
	}
	if _, leaked := got.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; leaked {
		t.Error("non-fleet annotation must not be returned")
	}
}

func TestAPIResourcesFilters(t *testing.T) {
	srv := newTestServer(t,
		apiTestVM("vm-a", "default", map[string]any{"stuttgart-things.com/source-path": "a.yaml"}),
		apiTestVM("vm-b", "default", nil),
		apiTestVM("vm-c", "other", map[string]any{"stuttgart-things.com/source-path": "c.yaml"}),
	)

	cases := []struct {
		url  string
		want []string
	}{
		{"/api/v1/resources?kind=HarvesterVM", []string{"vm-a", "vm-b", "vm-c"}},
		{"/api/v1/resources?kind=HarvesterVM&namespace=default", []string{"vm-a", "vm-b"}},
		{"/api/v1/resources?kind=HarvesterVM&annotation=stuttgart-things.com/source-path", []string{"vm-a", "vm-c"}},
		{"/api/v1/resources?namespace=other&annotation=stuttgart-things.com/source-path", []string{"vm-c"}},
	}
	for _, c := range cases {
		rr, list := getAPI(t, srv, c.url)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", c.url, rr.Code)
		}
		var names []string
		for _, it := range list.Items {
			names = append(names, it.Name)
		}
		if len(names) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.url, names, c.want)
			continue
		}
		for i := range names {
			if names[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v (sorted by name)", c.url, names, c.want)
				break
			}
		}
	}
}

func TestAPIResourcesEmptyIsArray(t *testing.T) {
	rr, _ := getAPI(t, newTestServer(t), "/api/v1/resources?kind=HarvesterVM")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Body.String(); got != "{\"items\":[]}\n" {
		t.Errorf("empty result must be an empty array, got %q", got)
	}
}

func TestAPIResourcesUnknownKind(t *testing.T) {
	rr, _ := getAPI(t, newTestServer(t), "/api/v1/resources?kind=NoSuchKind")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Errorf("expected JSON error body, got %q", rr.Body.String())
	}
}
