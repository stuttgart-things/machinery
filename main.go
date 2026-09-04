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
	"sync"
	"syscall"
	"time"

	resourceservice "github.com/stuttgart-things/machinery/resourceservice"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = os.Getenv("KUBECONFIG")
	configPath = os.Getenv("MACHINERY_CONFIG")
)

// informerResync is the dynamic informer factory's resync period. The
// watch keeps each cache fresh on its own; resync only re-delivers the
// full set to event handlers periodically — harmless here, and it gives
// long-lived WatchResources streams a periodic self-heal.
const informerResync = 10 * time.Minute

// informerRetryInterval is how often a kind the cluster did not serve
// at startup is probed again. A machinery cluster installs its
// Configurations through Crossplane, so the XRD CRDs appear seconds to
// minutes AFTER this process starts -- see
// stuttgart-things/clusterbook#186, where the pod came up 31s before the
// first CRD and served an empty table until it was restarted by hand.
//
// A var, not a const, so tests can shorten it; nothing else writes it.
var informerRetryInterval = 30 * time.Second

// watchBufferSize bounds the per-stream event backlog. A client that
// falls more than this many events behind gets ResourceExhausted and
// is expected to reconnect (which replays a fresh snapshot).
const watchBufferSize = 256

type server struct {
	resourceservice.UnimplementedResourceServiceServer
	config *Config
	// mu guards informers. It is written by retryUnservedKinds long
	// after startup, so every read goes through informer().
	mu sync.RWMutex
	// informers holds one shared informer per configured kind the
	// cluster serves. Reads use the informer's Lister (cache, no API
	// call); WatchResources attaches event handlers. A kind absent here
	// is not served by the cluster YET -- retryUnservedKinds keeps
	// probing and adds it when its CRD appears. Populated by
	// startInformers.
	informers map[string]informers.GenericInformer
}

// informer returns the informer for kind, if the cluster serves it.
func (s *server) informer(kind string) (informers.GenericInformer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inf, ok := s.informers[kind]
	return inf, ok
}

// addInformer publishes a late-arriving kind. Its cache must already be
// synced -- a Lister over an unsynced informer answers empty, which is
// the very failure this whole path exists to avoid.
func (s *server) addInformer(kind string, inf informers.GenericInformer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.informers[kind] = inf
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
	infs, informerFactory, unservedKinds := startInformers(dynamicClient, cfg, stopCh)

	addr := fmt.Sprintf(":%d", cfg.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "port", cfg.Port, "error", err)
		os.Exit(1)
	}

	srv := &server{config: cfg, informers: infs}

	// Kinds the cluster did not serve yet are not given up on: Crossplane
	// creates the XRD CRDs after this pod starts, and a one-shot discovery
	// left the dashboard permanently empty on a freshly built cluster
	// (stuttgart-things/clusterbook#186).
	go retryUnservedKinds(dynamicClient, srv, informerFactory, unservedKinds, stopCh)

	// Keepalive keeps long-lived WatchResources streams alive through
	// idle timeouts on the gateway/proxy in front of the server.
	grpcOpts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    2 * time.Minute,
			Timeout: 20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             time.Minute,
			PermitWithoutStream: true,
		}),
	}
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
		grpcOpts = append(grpcOpts,
			grpc.UnaryInterceptor(newAuthInterceptor(token)),
			grpc.StreamInterceptor(newAuthStreamInterceptor(token)),
		)
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
// for their caches to warm, and returns one informer per live kind.
// Kinds the API server does not serve (CRD absent, API version
// retired) are skipped with a warning — one missing kind must not
// stop the others, the same tolerance the old per-request List path
// had. The informers run until stopCh is closed.
//
// It also returns the shared factory and the kinds that were NOT served,
// so retryUnservedKinds can pick them up later. Not being served is
// usually TEMPORARY: on a machinery cluster the XRD CRDs are created by
// Crossplane seconds to minutes after this process starts.
func startInformers(dc dynamic.Interface, cfg *Config, stopCh <-chan struct{}) (
	map[string]informers.GenericInformer, dynamicinformer.DynamicSharedInformerFactory, []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dc, informerResync)
	infs := make(map[string]informers.GenericInformer, len(cfg.Resources))
	var unserved []string

	for kind, rk := range cfg.Resources {
		gvr := rk.toGVR()
		// One probe List per kind at startup (not per request): tells
		// us whether the cluster serves this GVR before we attach an
		// informer whose reflector would otherwise retry-spam forever.
		if _, err := dc.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				slog.Warn("kind not served by cluster yet, will retry",
					"kind", kind, "gvr", gvr.String(), "retryIn", informerRetryInterval)
				unserved = append(unserved, kind)
				continue
			}
			// Other errors (RBAC, transient API hiccup) — attach the
			// informer anyway; its reflector retries and the cache
			// fills in once access works.
			slog.Warn("kind probe failed, attaching informer anyway",
				"kind", kind, "gvr", gvr.String(), "error", err)
		}
		infs[kind] = factory.ForResource(gvr)
	}

	if len(infs) == 0 {
		// Not fatal and not permanent: this is the normal state of a
		// freshly built cluster whose Crossplane packages are still
		// installing. retryUnservedKinds will attach them.
		slog.Warn("no configured kinds are served by the cluster yet; resource queries are empty until they appear",
			"unserved", len(unserved), "retryIn", informerRetryInterval)
		return infs, factory, unserved
	}

	factory.Start(stopCh)
	slog.Info("waiting for informer cache sync", "kinds", len(infs))
	for typ, ok := range factory.WaitForCacheSync(ctx.Done()) {
		if !ok {
			slog.Warn("informer cache did not sync before timeout; results may be briefly incomplete",
				"type", typ.String())
		}
	}
	slog.Info("informer caches ready", "kinds", len(infs), "unserved", len(unserved))
	return infs, factory, unserved
}

// retryUnservedKinds re-probes the kinds the cluster did not serve at
// startup and attaches an informer as soon as one appears, so the
// process recovers on its own instead of needing a restart.
//
// It exists because discovery used to be a one-shot: a kind that was
// merely LATE — the normal case, since Crossplane creates the XRD CRDs
// after this pod is scheduled — was dropped exactly like a kind that had
// been retired, and every later request answered from an informer map
// that would never gain it. The asymmetry was the bug: any OTHER probe
// error already attached the informer and let its reflector recover.
//
// Runs until every pending kind is attached or stopCh closes.
func retryUnservedKinds(
	dc dynamic.Interface,
	srv *server,
	factory dynamicinformer.DynamicSharedInformerFactory,
	pending []string,
	stopCh <-chan struct{},
) {
	if len(pending) == 0 {
		return
	}
	ticker := time.NewTicker(informerRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}

		var still []string
		for _, kind := range pending {
			rk, ok := srv.config.Resources[kind]
			if !ok {
				continue // config changed under us; drop it
			}
			gvr := rk.toGVR()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := dc.Resource(gvr).List(ctx, metav1.ListOptions{Limit: 1})
			cancel()
			if err != nil && (apierrors.IsNotFound(err) || meta.IsNoMatchError(err)) {
				still = append(still, kind)
				continue
			}
			// Served now, or failing for a reason a reflector can retry
			// through (RBAC, API hiccup) — same tolerance as startup.
			if err != nil {
				slog.Warn("kind probe failed on retry, attaching informer anyway",
					"kind", kind, "gvr", gvr.String(), "error", err)
			}
			inf := factory.ForResource(gvr)
			// Start is additive: it starts informers the factory has not
			// started yet and leaves the running ones alone.
			factory.Start(stopCh)

			syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)
			synced := cache.WaitForCacheSync(syncCtx.Done(), inf.Informer().HasSynced)
			syncCancel()
			if !synced {
				// Publishing an unsynced informer would answer empty
				// from its Lister, which is the failure this replaces.
				slog.Warn("late kind did not sync in time, retrying", "kind", kind)
				still = append(still, kind)
				continue
			}
			srv.addInformer(kind, inf)
			slog.Info("kind became available, informer attached", "kind", kind, "gvr", gvr.String())
		}

		pending = still
		if len(pending) == 0 {
			slog.Info("all configured kinds are served; stopping discovery retry")
			return
		}
	}
}

// resolveKinds expands "" / "*" to every configured kind and validates
// each requested kind against the config. Shared by GetResources and
// WatchResources.
func (s *server) resolveKinds(kind string) ([]string, error) {
	if kind == "" || kind == "*" {
		kinds := make([]string, 0, len(s.config.Resources))
		for k := range s.config.Resources {
			kinds = append(kinds, k)
		}
		return kinds, nil
	}
	kinds := strings.Split(kind, ",")
	for _, k := range kinds {
		if _, ok := s.config.Resources[k]; !ok {
			supported := make([]string, 0, len(s.config.Resources))
			for sk := range s.config.Resources {
				supported = append(supported, sk)
			}
			return nil, status.Errorf(codes.InvalidArgument,
				"unsupported kind %q, valid kinds: %s", k, strings.Join(supported, ", "))
		}
	}
	return kinds, nil
}

// toResourceStatus projects a cached object into the gRPC
// ResourceStatus, applying the kind's connection/status/info field
// mappings. Shared by GetResources, GetResourceDetail and the watch.
func toResourceStatus(item *unstructured.Unstructured, kind string, rk ResourceKind) *resourceservice.ResourceStatus {
	statusMessage, ready := getResourceStatus(item)
	connDetails := getConnectionDetails(item, rk.ConnectionField)
	if len(rk.StatusFields) > 0 {
		if extra := getStatusDetails(item, rk.StatusFields); extra != "" {
			if connDetails != "" {
				connDetails = connDetails + " | " + extra
			} else {
				connDetails = extra
			}
		}
	}
	return &resourceservice.ResourceStatus{
		Name:              item.GetName(),
		Kind:              kind,
		Ready:             ready,
		StatusMessage:     statusMessage,
		ConnectionDetails: connDetails,
		Namespace:         item.GetNamespace(),
		InfoFields:        getInfoFields(item, rk.InfoFields),
	}
}

func (s *server) GetResources(ctx context.Context, req *resourceservice.ResourceRequest) (*resourceservice.ResourceListResponse, error) {
	if req.Count == 0 {
		req.Count = -1
	}
	if req.Count < -1 || req.Count > 1000 {
		return nil, status.Errorf(codes.InvalidArgument,
			"count must be between -1 (all) and 1000, got %d", req.Count)
	}

	kinds, err := s.resolveKinds(req.Kind)
	if err != nil {
		return nil, err
	}

	var allResources []*resourceservice.ResourceStatus
	for _, kind := range kinds {
		inf, ok := s.informer(kind)
		if !ok {
			// No informer for this kind — the cluster does not serve
			// it (CRD absent, API version retired). Skip, as the old
			// per-request List path did on IsNotFound/NoMatch.
			// retryUnservedKinds is probing it; if its CRD appears the
			// next request will find it.
			slog.Warn("kind has no informer cache, skipping", "kind", kind)
			continue
		}
		objs, err := inf.Lister().List(labels.Everything())
		if err != nil {
			return nil, fmt.Errorf("error listing cached resources for kind %s: %w", kind, err)
		}
		rk := s.config.Resources[kind]
		for _, obj := range objs {
			item, ok := obj.(*unstructured.Unstructured)
			if !ok {
				continue
			}
			allResources = append(allResources, toResourceStatus(item, kind, rk))
		}
	}

	// Informer stores are unordered; sort for a stable response so the
	// dashboard rows don't shuffle between polls — and so a count limit
	// returns a predictable top-N, not an arbitrary subset.
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

	// Cap to req.Count after sorting (count<0 means "all"). Applying it
	// here, not during iteration, keeps the limited set deterministic.
	if req.Count >= 0 && len(allResources) > int(req.Count) {
		allResources = allResources[:int(req.Count)]
	}

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

	inf, ok := s.informer(req.Kind)
	if !ok {
		return nil, status.Errorf(codes.Unavailable,
			"kind %q is not served by the cluster (yet); retrying every %s", req.Kind, informerRetryInterval)
	}
	lister := inf.Lister()

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

	return toResourceStatus(item, req.Kind, rk), nil
}

// WatchResources streams resource changes for the requested kind(s).
// Each kind's informer replays its current cache as ADDED events on
// subscribe, then live ADDED/MODIFIED/DELETED deltas follow until the
// client disconnects. ResourceRequest.count is ignored — a watch is
// unbounded by nature.
func (s *server) WatchResources(req *resourceservice.ResourceRequest, stream resourceservice.ResourceService_WatchResourcesServer) error {
	kinds, err := s.resolveKinds(req.Kind)
	if err != nil {
		return err
	}

	// Buffered so a brief slow patch on the client doesn't stall the
	// informer's shared delivery goroutine. On overflow the stream ends
	// with ResourceExhausted; the client reconnects and re-syncs rather
	// than silently missing events.
	events := make(chan *resourceservice.ResourceEvent, watchBufferSize)
	overflow := make(chan struct{}, 1)

	var registered []string
	for _, kind := range kinds {
		inf, ok := s.informer(kind)
		if !ok {
			slog.Warn("watch: kind has no informer cache, skipping", "kind", kind)
			continue
		}
		rk := s.config.Resources[kind]
		reg, err := inf.Informer().AddEventHandler(watchHandler(kind, rk, events, overflow))
		if err != nil {
			return status.Errorf(codes.Internal, "registering watch for kind %s: %v", kind, err)
		}
		defer func(kind string, inf informers.GenericInformer, reg cache.ResourceEventHandlerRegistration) {
			if err := inf.Informer().RemoveEventHandler(reg); err != nil {
				slog.Warn("watch: removing event handler", "kind", kind, "error", err)
			}
		}(kind, inf, reg)
		registered = append(registered, kind)
	}

	slog.Info("watch started", "kinds", registered)
	for {
		select {
		case <-stream.Context().Done():
			slog.Info("watch closed by client", "kinds", registered)
			return nil
		case <-overflow:
			return status.Error(codes.ResourceExhausted,
				"event backlog overflowed; reconnect to re-sync")
		case ev := <-events:
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// watchHandler builds informer callbacks that project each change into
// a ResourceEvent and feed it to the stream. Sends are non-blocking:
// the callbacks run on the informer's shared goroutine and must never
// block it, so an overflowing buffer signals overflow instead.
func watchHandler(kind string, rk ResourceKind, events chan<- *resourceservice.ResourceEvent, overflow chan<- struct{}) cache.ResourceEventHandlerFuncs {
	emit := func(t resourceservice.EventType, obj any) {
		item, ok := asUnstructured(obj)
		if !ok {
			return
		}
		ev := &resourceservice.ResourceEvent{Type: t, Resource: toResourceStatus(item, kind, rk)}
		select {
		case events <- ev:
		default:
			select {
			case overflow <- struct{}{}:
			default:
			}
		}
	}
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { emit(resourceservice.EventType_ADDED, obj) },
		UpdateFunc: func(_, newObj any) { emit(resourceservice.EventType_MODIFIED, newObj) },
		DeleteFunc: func(obj any) { emit(resourceservice.EventType_DELETED, obj) },
	}
}

// asUnstructured unwraps the object an informer hands a callback,
// including the DeletedFinalStateUnknown tombstone a delete delivers
// when the final state was missed.
func asUnstructured(obj any) (*unstructured.Unstructured, bool) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	item, ok := obj.(*unstructured.Unstructured)
	return item, ok
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
