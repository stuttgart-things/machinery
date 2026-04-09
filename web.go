package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
)

//go:embed web/templates/*.html
var templateFS embed.FS

type webServer struct {
	srv       *server
	templates *template.Template
}

type buildInfo struct {
	Version string
	Commit  string
	Date    string
}

type indexData struct {
	Kinds []string
	Build buildInfo
}

type resourceData struct {
	Name              string
	Kind              string
	Namespace         string
	Ready             bool
	StatusMessage     string
	ConnectionDetails string
	InfoFields        map[string]string
}

type resourceListData struct {
	Resources []resourceData
}

type detailData struct {
	Resource resourceData
}

func newWebServer(srv *server) (*webServer, error) {
	tmpl, err := template.ParseFS(templateFS, "web/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}
	return &webServer{srv: srv, templates: tmpl}, nil
}

func (w *webServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", w.handleIndex)
	mux.HandleFunc("GET /resources", w.handleResources)
	mux.HandleFunc("GET /detail", w.handleDetail)
	mux.HandleFunc("GET /health", w.handleHealth)
	return mux
}

func (w *webServer) handleIndex(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(rw, r)
		return
	}

	kinds := make([]string, 0, len(w.srv.config.Resources))
	for k := range w.srv.config.Resources {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	bi := buildInfo{Version: version, Commit: commit, Date: date}
	if len(bi.Commit) > 7 {
		bi.Commit = bi.Commit[:7]
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := w.templates.ExecuteTemplate(rw, "index.html", indexData{Kinds: kinds, Build: bi}); err != nil {
		slog.Error("failed to render index", "error", err)
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (w *webServer) handleResources(rw http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "*"
	}

	resp, err := w.srv.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Kind:  kind,
		Count: -1,
	})
	if err != nil {
		slog.Error("failed to get resources for web", "kind", kind, "error", err)
		http.Error(rw, "Failed to fetch resources", http.StatusInternalServerError)
		return
	}

	data := resourceListData{}
	for _, res := range resp.Resources {
		data.Resources = append(data.Resources, resourceData{
			Name:              res.Name,
			Kind:              res.Kind,
			Namespace:         res.Namespace,
			Ready:             res.Ready,
			StatusMessage:     res.StatusMessage,
			ConnectionDetails: res.ConnectionDetails,
			InfoFields:        res.InfoFields,
		})
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := w.templates.ExecuteTemplate(rw, "resources.html", data); err != nil {
		slog.Error("failed to render resources", "error", err)
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (w *webServer) handleDetail(rw http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	ns := r.URL.Query().Get("namespace")

	if kind == "" || name == "" {
		http.Error(rw, "kind and name are required", http.StatusBadRequest)
		return
	}

	resp, err := w.srv.GetResourceDetail(context.Background(), &resourceservice.ResourceDetailRequest{
		Kind:      kind,
		Name:      name,
		Namespace: ns,
	})
	if err != nil {
		slog.Error("failed to get resource detail", "kind", kind, "name", name, "error", err)
		http.Error(rw, "Resource not found", http.StatusNotFound)
		return
	}

	data := detailData{
		Resource: resourceData{
			Name:              resp.Name,
			Kind:              resp.Kind,
			Namespace:         resp.Namespace,
			Ready:             resp.Ready,
			StatusMessage:     resp.StatusMessage,
			ConnectionDetails: resp.ConnectionDetails,
			InfoFields:        resp.InfoFields,
		},
	}

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := w.templates.ExecuteTemplate(rw, "detail.html", data); err != nil {
		slog.Error("failed to render detail", "error", err)
		http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (w *webServer) handleHealth(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/plain")
	rw.WriteHeader(http.StatusOK)
	fmt.Fprint(rw, "ok")
}
