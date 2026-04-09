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
	"strings"
	"syscall"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = os.Getenv("KUBECONFIG")
	configPath = os.Getenv("MACHINERY_CONFIG")
)

type server struct {
	resourceservice.UnimplementedResourceServiceServer
	dynamicClient dynamic.Interface
	config        *Config
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

	addr := fmt.Sprintf(":%d", cfg.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", "port", cfg.Port, "error", err)
		os.Exit(1)
	}

	srv := &server{dynamicClient: dynamicClient, config: cfg}

	s := grpc.NewServer()
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
		gvr := rk.toGVR()

		slog.Debug("fetching resources", "kind", kind, "gvr", gvr.Resource)

		resourceList, err := s.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("error fetching resources for kind %s: %w", kind, err)
		}

		for _, item := range resourceList.Items {
			if req.Count == 0 {
				break
			}
			statusMessage, ready := getResourceStatus(&item)

			connDetails := getConnectionDetails(&item, rk.ConnectionField)
			if len(rk.StatusFields) > 0 {
				extra := getStatusDetails(&item, rk.StatusFields)
				if extra != "" {
					if connDetails != "" {
						connDetails = connDetails + " | " + extra
					} else {
						connDetails = extra
					}
				}
			}

			infoFields := getInfoFields(&item, rk.InfoFields)

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

	slog.Info("resources fetched", "count", len(allResources))
	return &resourceservice.ResourceListResponse{Resources: allResources}, nil
}

func getResourceStatus(obj *unstructured.Unstructured) (string, bool) {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return fmt.Sprintf("Error reading conditions: %v", err), false
	}
	if !found {
		return "No conditions found", false
	}

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

	gvr := rk.toGVR()
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	item, err := s.dynamicClient.Resource(gvr).Namespace(ns).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "resource %s/%s not found: %v", req.Kind, req.Name, err)
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

	return ""
}
