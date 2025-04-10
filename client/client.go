package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	secureConnection  = os.Getenv("SECURE_CONNECTION")  // Read from env: "true" or "false"
	clusterBookServer = os.Getenv("CLUSTERBOOK_SERVER") // localhost:50051
)

func main() {
	// Connect to the gRPC server
	conn, err := grpc.NewClient(clusterBookServer, getCredentials())
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer conn.Close()

	// Create a client
	client := resourceservice.NewResourceServiceClient(conn)

	// Request to get all resources
	resp, err := client.GetResources(context.Background(), &resourceservice.ResourceRequest{
		Count: 5,                  // -1 = All resources
		Kind:  "VsphereVMAnsible", //"*", //"VsphereVMAnsible",
	})
	if err != nil {
		log.Fatalf("Error getting resources: %v", err)
	}

	// Print the result
	fmt.Println("Resources:")
	for _, resource := range resp.Resources {
		fmt.Printf("Name: %s, Kind: %s, Ready: %t, Status: %s, ConnectionDetails: %s\n", resource.Name, resource.Kind, resource.Ready, resource.StatusMessage, resource.ConnectionDetails)
	}
}

func getCredentials() grpc.DialOption {
	switch secureConnection {
	case "true":
		log.Println("Using secure gRPC connection")
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true, // Adjust based on your security requirements
		}
		return grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	case "false":
		log.Println("Using insecure gRPC connection")
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	default:
		log.Fatalf("Invalid SECURE_CONNECTION value: %s. Expected 'true' or 'false'", secureConnection)
		return nil // This will never be reached since log.Fatalf exits the program
	}
}
