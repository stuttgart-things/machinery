package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = os.Getenv("KUBECONFIG")

	supportedKinds = map[string]schema.GroupVersionResource{
		"AnsibleRun": {
			Group:    "resources.stuttgart-things.com",
			Version:  "v1alpha1",
			Resource: "ansibleruns",
		},
		"VsphereVMAnsible": {
			Group:    "resources.stuttgart-things.com",
			Version:  "v1alpha1",
			Resource: "vspherevmansibles",
		},
		"ProxmoxVMAnsible": {
			Group:    "resources.stuttgart-things.com",
			Version:  "v1alpha1",
			Resource: "proxmoxvmansibles",
		},
	}
)

type server struct {
	resourceservice.UnimplementedResourceServiceServer
	dynamicClient dynamic.Interface
}

func main() {
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		slog.Error("failed to load kubeconfig", "error", err)
		os.Exit(1)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		slog.Error("failed to create dynamic client", "error", err)
		os.Exit(1)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		slog.Error("failed to listen", "port", 50051, "error", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	resourceservice.RegisterResourceServiceServer(s, &server{
		dynamicClient: dynamicClient,
	})

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(s, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig)
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		s.GracefulStop()
	}()

	slog.Info("gRPC server listening", "port", 50051)
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

func (s *server) GetResources(ctx context.Context, req *resourceservice.ResourceRequest) (*resourceservice.ResourceListResponse, error) {
	// Validate and apply defaults for Count
	if req.Count == 0 {
		req.Count = -1 // all resources
	}
	if req.Count < -1 || req.Count > 1000 {
		return nil, status.Errorf(codes.InvalidArgument,
			"count must be between -1 (all) and 1000, got %d", req.Count)
	}

	// Validate Kind
	if req.Kind == "" || req.Kind == "*" {
		req.Kind = "AnsibleRun,VsphereVMAnsible"
	}

	kinds := strings.Split(req.Kind, ",")
	for _, kind := range kinds {
		if _, ok := supportedKinds[kind]; !ok {
			supported := make([]string, 0, len(supportedKinds))
			for k := range supportedKinds {
				supported = append(supported, k)
			}
			return nil, status.Errorf(codes.InvalidArgument,
				"unsupported kind %q, valid kinds: %s", kind, strings.Join(supported, ", "))
		}
	}

	var allResources []*resourceservice.ResourceStatus

	for _, kind := range kinds {
		gvr := supportedKinds[kind]

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

			allResources = append(allResources, &resourceservice.ResourceStatus{
				Name:              item.GetName(),
				Kind:              kind,
				Ready:             ready,
				StatusMessage:     statusMessage,
				ConnectionDetails: getIps(&item),
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

func getIps(obj *unstructured.Unstructured) string {
	share, found, err := unstructured.NestedMap(obj.Object, "status", "share")
	if err != nil {
		slog.Error("error reading share status", "resource", obj.GetName(), "error", err)
		return "ERROR READING STATUS"
	}
	if !found {
		return "NO STATUS FOUND"
	}

	ips, found, err := unstructured.NestedString(share, "ips")
	if err != nil {
		slog.Error("error reading IPs from share", "resource", obj.GetName(), "error", err)
		return "ERROR READING IPS"
	}
	if !found {
		return "NO IPS FOUND IN STATUS"
	}

	return ips
}
