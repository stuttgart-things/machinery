package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = os.Getenv("KUBECONFIG")
	configPath = os.Getenv("MACHINERY_CONFIG")
)

// informerResync is the dynamic informer factory's resync period. The
// watch keeps each cache fresh on its own; resync only re-delivers the
// full set to event handlers periodically — harmless here, and a sane
// default for the WatchResources work that builds on this cache.
const informerResync = 10 * time.Minute

type server struct {
	resourceservice.UnimplementedResourceServiceServer
	config *Config
	// listers holds one cache-backed lister per configured kind the
	// cluster actually serves. Populated by startInformers; reads never
	// hit the API server. A kind absent here was not served at startup.
	listers map[string]cache.GenericLister
}

func main() {
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Load configuration
	var cfg *Config
	if configPath != "" {
		var err error
		cfg, err = loadConfig(configPath)
		if err != nil {
			slog.Error("failed to load config", "path", configPath, "error", err)
			os.Exit(1)
		}
		slog.Info("loaded config from file", "path", configPath, "resources", len(cfg.Resources))
	} else {
		cfg = defaultConfig()
		slog.Info("using default config", "resources", len(cfg.Resources))
	}

	k8sConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		slog.Error("failed to load kubeconfig", "error", err)
		os.Exit(1)
	}
	dynamicClient, err := dynamic.NewForConfig(k8sConfig)
	if err != nil {
		slog.Error("failed to create dynamic client", "error", err)
		os.Exit(1)
	}

	// Informer caches replace the old per-request List/Get against the
	// API server: one watch per configured kind, shared by every gRPC
	// call and every dashboard poll. stopCh tears them down on shutdown.
	// This blocks until the caches warm, so the gRPC/HTTP servers below
	// only start serving once reads can be answered from cache.
	stopCh := make(chan struct{})
	listers := startInformers(dynamicClient, cfg, stopCh)

	addr := fmt.Sprintf(":%d", cfg.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "port", cfg.Port, "error", err)
		os.Exit(1)
	}

	srv := &server{config: cfg, listers: listers}

	var grpcOpts []grpc.ServerOption
	if cfg.Auth.Enabled {
		token, err := resolveAuthToken(cfg.Auth)
		if err != nil {
			slog.Error("failed to resolve auth token", "error", err)
			os.Exit(1)
		}
		if token == "" {
			slog.Error("auth enabled but no token resolved", "tokenFile", cfg.Auth.TokenFile, "tokenEnvVar", cfg.Auth.TokenEnvVar)
			os.Exit(1)
		}
		grpcOpts = append(grpcOpts, grpc.UnaryInterceptor(newAuthInterceptor(token)))
		slog.Info("gRPC auth enabled (bearer token)")
	}

	s := grpc.NewServer(grpcOpts...)
	resourceservice.RegisterResourceServiceServer(s, srv)

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(s, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	// Start HTTP server (HTMX frontend)
	webSrv, err := newWebServer(srv)
	if err != nil {
		slog.Error("failed to initialize web server", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HttpPort),
		Handler: webSrv.handler(),
	}

	go func() {
		slog.Info("HTTP server listening", "port", cfg.HttpPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig)
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		close(stopCh)
		httpServer.Shutdown(context.Background())
		s.GracefulStop()
	}()

	slog.Info("gRPC server listening", "port", cfg.Port)
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

// startInformers probes each configured kind, attaches a dynamic
// informer for the ones the cluster actually serves, waits (bounded)
// for their caches to warm, and returns one lister per live kind.
// Kinds the API server does not serve (CRD absent, API version
// retired) are skipped with a warning — one missing kind must not
// stop the others, the same tolerance the old per-request List path
// had. The informers run until stopCh is closed.
func startInformers(dc dynamic.Interface, cfg *Config, stopCh <-chan struct{}) map[string]cache.GenericLister {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dc, informerResync)
	listers := make(map[string]cache.GenericLister, len(cfg.Resources))

	for kind, rk := range cfg.Resources {
		gvr := rk.toGVR()
		// One probe List per kind at startup (not per request): tells
		// us whether the cluster serves this GVR before we attach an
		// informer whose reflector would otherwise retry-spam forever.
		if _, err := dc.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				slog.Warn("kind not served by cluster, skipping",
					"kind", kind, "gvr", gvr.String())
				continue
			}
			// Other errors (RBAC, transient API hiccup) — attach the
			// informer anyway; its reflector retries and the lister
			// fills in once access works.
			slog.Warn("kind probe failed, attaching informer anyway",
				"kind", kind, "gvr", gvr.String(), "error", err)
		}
		listers[kind] = factory.ForResource(gvr).Lister()
	}

	if len(listers) == 0 {
		slog.Warn("no configured kinds are served by the cluster; resource queries will be empty")
		return listers
	}

	factory.Start(stopCh)
	slog.Info("waiting for informer cache sync", "kinds", len(listers))
	for typ, ok := range factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			slog.Warn("informer cache did not sync before timeout; results may be briefly incomplete",
				"type", typ.String())
		}
	}
	slog.Info("informer caches ready", "kinds", len(listers))
	return listers
}

func (s *server) GetResources(ctx context.Context, req *resourceservice.ResourceRequest) (*resourceservice.ResourceListResponse, error) {
	if req.Count == 0 {
		req.Count = -1
	}
	if req.Count < -1 || req.Count > 1000 {
		return nil, status.Errorf(codes.InvalidArgument,
			"count must be between -1 (all) and 1000, got %d", req.Count)
	}

	if req.Kind == "" || req.Kind == "*" {
		kinds := make([]string, 0, len(s.config.Resources))
		for k := range s.config.Resources {
			kinds = append(kinds, k)
		}
		req.Kind = strings.Join(kinds, ",")
	}

	kinds := strings.Split(req.Kind, ",")
	for _, kind := range kinds {
		if _, ok := s.config.Resources[kind]; !ok {
			supported := make([]string, 0, len(s.config.Resources))
			for k := range s.config.Resources {
				supported = append(supported, k)
			}
			return nil, status.Errorf(codes.InvalidArgument,
				"unsupported kind %q, valid kinds: %s", kind, strings.Join(supported, ", "))
		}
	}

	var allResources []*resourceservice.ResourceStatus

	for _, kind := range kinds {
		rk := s.config.Resources[kind]

		lister, ok := s.listers[kind]
		if !ok {
			// No informer for this kind — the cluster did not serve it
			// at startup (CRD removed, API version retired). Skip, as
			// the old per-request List path did on IsNotFound/NoMatch.
			slog.Warn("kind has no informer cache, skipping", "kind", kind)
			continue
		}

		objs, err := lister.List(labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("error listing cached resources for kind %s: %w", kind, err)
		}

		for _, obj := range objs {
			if req.Count == 0 {
				break
			}
			item, ok := obj.(*unstructured.Unstructured)
			if !ok {
				continue
			}

			statusMessage, ready := getResourceStatus(item)

			connDetails := getConnectionDetails(item, rk.ConnectionField)
			if len(rk.StatusFields) > 0 {
				extra := getStatusDetails(item, rk.StatusFields)
				if extra != "" {
					if connDetails != "" {
						connDetails = connDetails + " | " + extra
					} else {
						connDetails = extra
					}
				}
			}

			infoFields := getInfoFields(item, rk.InfoFields)

			allResources = append(allResources, &resourceservice.ResourceStatus{
				Name:              item.GetName(),
				Kind:              kind,
				Ready:             ready,
				StatusMessage:     statusMessage,
				ConnectionDetails: connDetails,
				Namespace:         item.GetNamespace(),
				InfoFields:        infoFields,
			})

			if req.Count > 0 {
				req.Count--
			}
		}
	}

	// Informer stores are unordered; sort for a stable response so the
	// dashboard rows don't shuffle between polls.
	sort.Slice(allResources, func(i, j int) bool {
		a, b := allResources[i], allResources[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})

	slog.Info("resources fetched", "count", len(allResources))
	return &resourceservice.ResourceListResponse{Resources: allResources}, nil
}

func getResourceStatus(obj *unstructured.Unstructured) (string, bool) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return fmt.Sprintf("Error reading conditions: %v", err), false
	}
	if found {
		for _, c := range conditions {
			condition, ok := c.(map[string]any)
			if !ok {
				continue
			}

			if condition["type"] == "Ready" {
				if condition["status"] == "True" {
					return "Ready", true
				}
				return "Not Ready", false
			}
		}
		return "Not Ready", false
	}

	// Gateway API kinds (HTTPRoute, GRPCRoute, …) scope conditions per
	// parent at status.parents[*].conditions. There is no "Ready" type;
	// per the spec readiness is Accepted=True + ResolvedRefs=True on
	// every attached parent. Report Ready iff every condition is True,
	// otherwise return the first non-True condition as the message.
	parents, parentsFound, err := unstructured.NestedSlice(obj.Object, "status", "parents")
	if err != nil {
		return fmt.Sprintf("Error reading parents: %v", err), false
	}
	if !parentsFound {
		return "No conditions found", false
	}
	var seen int
	for _, p := range parents {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		pconds, ok2, err := unstructured.NestedSlice(pm, "conditions")
		if err != nil || !ok2 {
			continue
		}
		for _, c := range pconds {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			seen++
			if cm["status"] != "True" {
				t, _ := cm["type"].(string)
				r, _ := cm["reason"].(string)
				if r != "" {
					return fmt.Sprintf("%s: %s", t, r), false
				}
				return t, false
			}
		}
	}
	if seen == 0 {
		return "No conditions found", false
	}
	return "Ready", true
}

func getConnectionDetails(obj *unstructured.Unstructured, fieldPath string) string {
	if fieldPath == "" {
		return ""
	}
	return getNestedField(obj, fieldPath)
}

func getStatusDetails(obj *unstructured.Unstructured, fields []string) string {
	var parts []string
	for _, field := range fields {
		val := getNestedField(obj, field)
		if val != "" {
			// Use the last segment of the path as the label
			segments := strings.Split(field, ".")
			label := segments[len(segments)-1]
			parts = append(parts, label+"="+val)
		}
	}
	return strings.Join(parts, ", ")
}

func getInfoFields(obj *unstructured.Unstructured, fields []InfoField) map[string]string {
	result := make(map[string]string)
	for _, f := range fields {
		val := getNestedField(obj, f.Path)
		if val != "" {
			result[f.Label] = val
		}
	}
	return result
}

func (s *server) GetResourceDetail(ctx context.Context, req *resourceservice.ResourceDetailRequest) (*resourceservice.ResourceStatus, error) {
	rk, ok := s.config.Resources[req.Kind]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported kind %q", req.Kind)
	}

	lister, ok := s.listers[req.Kind]
	if !ok {
		return nil, status.Errorf(codes.Unavailable, "kind %q is not served by the cluster", req.Kind)
	}

	// Namespaced lookups key on namespace/name; cluster-scoped (or a
	// caller that omits the namespace) on name alone.
	var obj runtime.Object
	var err error
	if req.Namespace != "" {
		obj, err = lister.ByNamespace(req.Namespace).Get(req.Name)
	} else {
		obj, err = lister.Get(req.Name)
	}
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "resource %s/%s not found: %v", req.Kind, req.Name, err)
	}

	item, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, status.Errorf(codes.Internal, "unexpected cached object type for %s/%s", req.Kind, req.Name)
	}

	statusMessage, ready := getResourceStatus(item)
	connDetails := getConnectionDetails(item, rk.ConnectionField)
	if len(rk.StatusFields) > 0 {
		extra := getStatusDetails(item, rk.StatusFields)
		if extra != "" {
			if connDetails != "" {
				connDetails = connDetails + " | " + extra
			} else {
				connDetails = extra
			}
		}
	}
	infoFields := getInfoFields(item, rk.InfoFields)

	return &resourceservice.ResourceStatus{
		Name:              item.GetName(),
		Kind:              req.Kind,
		Ready:             ready,
		StatusMessage:     statusMessage,
		ConnectionDetails: connDetails,
		Namespace:         item.GetNamespace(),
		InfoFields:        infoFields,
	}, nil
}

func getNestedField(obj *unstructured.Unstructured, fieldPath string) string {
	segments := strings.Split(fieldPath, ".")

	// Try as string first
	val, found, err := unstructured.NestedString(obj.Object, segments...)
	if err == nil && found {
		return val
	}

	// Try as bool
	boolVal, found, err := unstructured.NestedBool(obj.Object, segments...)
	if err == nil && found {
		return fmt.Sprintf("%v", boolVal)
	}

	// Try as int64
	intVal, found, err := unstructured.NestedInt64(obj.Object, segments...)
	if err == nil && found {
		return fmt.Sprintf("%d", intVal)
	}

	// Slice fallback. Gateway API surfaces useful fields as arrays:
	// []string (e.g. HTTPRoute.spec.hostnames) and []map (e.g.
	// HTTPRoute.spec.parentRefs). Render strings joined, maps as
	// namespace/name pairs so the UI shows something usable.
	slice, found, err := unstructured.NestedSlice(obj.Object, segments...)
	if err == nil && found {
		var parts []string
		for _, item := range slice {
			switch v := item.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if s := summarizeRef(v); s != "" {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, ", ")
	}

	return ""
}

func summarizeRef(m map[string]any) string {
	name, _ := m["name"].(string)
	ns, _ := m["namespace"].(string)
	if name != "" && ns != "" {
		return fmt.Sprintf("%s/%s", ns, name)
	}
	if name != "" {
		return name
	}
	return ""
}
