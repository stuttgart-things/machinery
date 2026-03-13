package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"

	"google.golang.org/grpc"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = os.Getenv("KUBECONFIG")
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

	slog.Info("gRPC server listening", "port", 50051)
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		os.Exit(1)
	}
}

func (s *server) GetResources(ctx context.Context, req *resourceservice.ResourceRequest) (*resourceservice.ResourceListResponse, error) {
	if req.Count == 0 {
		req.Count = -1
	}

	if req.Kind == "" || req.Kind == "*" {
		req.Kind = "AnsibleRun,VsphereVMAnsible"
	}

	kinds := strings.Split(req.Kind, ",")
	var allResources []*resourceservice.ResourceStatus

	for _, kind := range kinds {
		var gvr schema.GroupVersionResource
		switch kind {
		case "AnsibleRun":
			gvr = schema.GroupVersionResource{
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "ansibleruns",
			}
		case "VsphereVMAnsible":
			gvr = schema.GroupVersionResource{
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "vspherevmansibles",
			}
		case "ProxmoxVMAnsible":
			gvr = schema.GroupVersionResource{
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "proxmoxvmansibles",
			}
		default:
			slog.Warn("skipping unknown resource kind", "kind", kind)
			continue
		}

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
