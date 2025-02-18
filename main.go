package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"

	"google.golang.org/grpc"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

type server struct {
	resourceservice.UnimplementedResourceServiceServer
	dynamicClient dynamic.Interface
}

func main() {
	kubeconfig := flag.String("kubeconfig", "/home/sthings/.kube/manager-dev", "Path to the kubeconfig file")
	flag.Parse()

	// Initialize Kubernetes client
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("Error loading kubeconfig: %v", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating dynamic client: %v", err)
	}

	// Start the gRPC server
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen on port 50051: %v", err)
	}

	s := grpc.NewServer()
	resourceservice.RegisterResourceServiceServer(s, &server{
		dynamicClient: dynamicClient,
	})

	// Start the server
	fmt.Println("gRPC server listening on port 50051...")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func (s *server) GetResources(ctx context.Context, req *resourceservice.ResourceRequest) (*resourceservice.ResourceListResponse, error) {
	// Default values
	if req.Count == 0 {
		req.Count = -1 // All resources
	}

	if req.Kind == "" || req.Kind == "*" {
		req.Kind = "AnsibleRun,VsphereVMAnsible" // Add other kinds here
	}

	kinds := strings.Split(req.Kind, ",") // Allow comma-separated kinds
	var allResources []*resourceservice.ResourceStatus

	// Fetch resources for the given kinds
	for _, kind := range kinds {
		// Construct GVR (GroupVersionResource) based on the kind
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
		default:
			// Skip unknown kinds
			continue
		}

		// Fetch the resources for this kind across all namespaces
		resourceList, err := s.dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("error fetching resources: %v", err)
		}

		// Process each resource and limit by count
		for _, item := range resourceList.Items {
			if req.Count == 0 {
				break
			}
			statusMessage, ready := getResourceStatus(&item)

			// Append pointer to ResourceStatus
			allResources = append(allResources, &resourceservice.ResourceStatus{
				Name:              item.GetName(),
				Kind:              kind,
				Ready:             ready,
				StatusMessage:     statusMessage,
				ConnectionDetails: PrintIps(&item),
			})

			if req.Count > 0 {
				req.Count--
			}
		}
	}

	// Return the result
	return &resourceservice.ResourceListResponse{Resources: allResources}, nil
}

func getResourceStatus(obj *unstructured.Unstructured) (string, bool) {
	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return "No conditions found", false
	}

	for _, c := range conditions {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// Look for Type: "Ready" with Status: "True"
		if condition["type"] == "Ready" && condition["status"] == "True" {
			return "Ready", true

		}

		return "Not Ready", false
	}

	return "Not Ready", false
}

func PrintIps(obj *unstructured.Unstructured) string {

	// Retrieve the "Ips" field from the Share section of the status
	share, found, _ := unstructured.NestedMap(obj.Object, "status", "share")
	if !found {
		fmt.Println("ℹ️ No Share information found.")
		return "NO STATUS FOUND"
	}

	ips, found, _ := unstructured.NestedString(share, "ips")
	if found {
		fmt.Printf("🌐 VsphereVMAnsible IPs: %s\n", ips)
		return ips

	} else {
		fmt.Println("ℹ️ No IPs found in Share section.")
		return "NO IPS FOUND IN STATUS"
	}
}
