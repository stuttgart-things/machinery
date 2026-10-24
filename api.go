package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
)

// annotationPrefix selects the annotations the JSON API returns. Only the
// fleet's own keys (stuttgart-things.com/source-path, /management-cluster,
// ...) go out: kubectl's last-applied-configuration and similar carry the
// whole object, sometimes with secrets in it, and no consumer needs them.
const annotationPrefix = "stuttgart-things.com/"

// apiResource is one XR as GET /api/v1/resources returns it. The
// annotations are what a consumer needs to act on the object in git:
// source-path names the file the XR lives in, management-cluster the
// plane that reconciles it (stuttgart-things#4073).
type apiResource struct {
	Kind              string            `json:"kind"`
	APIVersion        string            `json:"apiVersion"`
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	Ready             bool              `json:"ready"`
	StatusMessage     string            `json:"statusMessage,omitempty"`
	CreationTimestamp string            `json:"creationTimestamp,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

type apiResourceList struct {
	Items []apiResource `json:"items"`
}

// handleAPIResources serves the informer cache as JSON for programmatic
// clients (the Backstage claim picker). Filters, all optional:
//
//	kind=ClusterStack,NativeVsphereVM   (default: every watched kind)
//	namespace=default                    (comma-separated)
//	annotation=stuttgart-things.com/source-path   (only objects carrying it)
func (w *webServer) handleAPIResources(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kinds, err := w.srv.resolveKinds(q.Get("kind"))
	if err != nil {
		writeJSONError(rw, http.StatusBadRequest, status.Convert(err).Message())
		return
	}
	nsFilter := parseCSV(q.Get("namespace"))
	mustHave := strings.TrimSpace(q.Get("annotation"))

	out := apiResourceList{Items: []apiResource{}}
	for _, kind := range kinds {
		inf, ok := w.srv.informer(kind)
		if !ok {
			// Kind not served by the cluster (yet): same as GetResources.
			continue
		}
		objs, err := inf.Lister().List(labels.Everything())
		if err != nil {
			slog.Error("failed to list cached resources for api", "kind", kind, "error", err)
			writeJSONError(rw, http.StatusInternalServerError, "failed to list "+kind)
			return
		}
		rk := w.srv.config.Resources[kind]
		for _, obj := range objs {
			item, ok := obj.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			if len(nsFilter) > 0 {
				if _, ok := nsFilter[item.GetNamespace()]; !ok {
					continue
				}
			}
			ann := fleetAnnotations(item)
			if mustHave != "" {
				if _, ok := ann[mustHave]; !ok {
					continue
				}
			}
			st := toResourceStatus(item, kind, rk)
			out.Items = append(out.Items, apiResource{
				Kind:              kind,
				APIVersion:        item.GetAPIVersion(),
				Name:              st.Name,
				Namespace:         st.Namespace,
				Ready:             st.Ready,
				StatusMessage:     st.StatusMessage,
				CreationTimestamp: st.CreationTimestamp,
				Annotations:       ann,
			})
		}
	}

	// Same order as GetResources: kind, namespace, name.
	sort.Slice(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})

	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(out); err != nil {
		slog.Error("failed to encode api resources", "error", err)
	}
}

// fleetAnnotations returns the object's annotations under annotationPrefix,
// or nil when it has none.
func fleetAnnotations(item *unstructured.Unstructured) map[string]string {
	var out map[string]string
	for k, v := range item.GetAnnotations() {
		if strings.HasPrefix(k, annotationPrefix) {
			if out == nil {
				out = map[string]string{}
			}
			out[k] = v
		}
	}
	return out
}

func writeJSONError(rw http.ResponseWriter, code int, msg string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(code)
	_ = json.NewEncoder(rw).Encode(map[string]string{"error": msg})
}
