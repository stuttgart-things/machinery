package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"

	resourceservice "github.com/stuttgart-things/machinery/resourceservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	secureConnection  = os.Getenv("SECURE_CONNECTION")    // "true" or "false"
	clusterBookServer = os.Getenv("CLUSTERBOOK_SERVER")   // localhost:50051
	tlsCACertPath     = os.Getenv("TLS_CA_CERT")          // optional: path to CA cert for verification
	tlsSkipVerify     = os.Getenv("TLS_SKIP_VERIFY")      // "true" only for development
	authToken         = os.Getenv("MACHINERY_AUTH_TOKEN") // optional: bearer token; attached as `authorization: Bearer <token>` when set
)

func main() {
	dialOpts := []grpc.DialOption{getCredentials()}
	if authToken != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(bearerToken{
			token:    authToken,
			insecure: secureConnection != "true",
		}))
	}

	conn, err := grpc.NewClient(clusterBookServer, dialOpts...)
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

// bearerToken implements grpc.PerRPCCredentials, attaching
// `authorization: Bearer <token>` to every outgoing RPC. RequireTransportSecurity
// is reported per-instance so the same struct works on TLS and plaintext dial
// paths — flip insecure=true to allow sending the token without TLS (LAN/dev).
type bearerToken struct {
	token    string
	insecure bool
}

func (b bearerToken) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearerToken) RequireTransportSecurity() bool {
	return !b.insecure
}

func getCredentials() grpc.DialOption {
	switch secureConnection {
	case "true":
		tlsConfig := &tls.Config{}

		if tlsSkipVerify == "true" {
			log.Println("WARNING: TLS certificate verification disabled (dev mode)")
			tlsConfig.InsecureSkipVerify = true
		} else if tlsCACertPath != "" {
			caCert, err := os.ReadFile(tlsCACertPath)
			if err != nil {
				log.Fatalf("Failed to read CA certificate: %v", err)
			}
			certPool := x509.NewCertPool()
			if !certPool.AppendCertsFromPEM(caCert) {
				log.Fatalf("Failed to parse CA certificate")
			}
			tlsConfig.RootCAs = certPool
			log.Println("Using secure gRPC connection with custom CA")
		} else {
			log.Println("Using secure gRPC connection with system CA pool")
		}

		return grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig))
	case "false", "":
		log.Println("Using insecure gRPC connection")
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	default:
		log.Fatalf("Invalid SECURE_CONNECTION value: %s. Expected 'true' or 'false'", secureConnection)
		return nil
	}
}
