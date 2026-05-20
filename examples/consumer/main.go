// Command consumer is an example client for the machinery gRPC service.
// It demonstrates the two RPCs (GetResources / GetResourceDetail), the
// health probe, and the connection plumbing (plaintext, TLS with a custom
// CA, bearer-token auth) downstream consumers are expected to wire up.
//
// It is deliberately a single file so the relevant snippets (dial,
// bearerToken, error handling) can be copied into another service.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	resourceservice "github.com/stuttgart-things/maschinist/resourceservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const usage = `consumer — example client for the machinery gRPC service.

Usage:
  consumer <command> [flags]

Commands:
  list      Call GetResources (machinery's "browse" endpoint)
  get       Call GetResourceDetail (single CR with info fields)
  health    Call grpc.health.v1.Health/Check (does the route answer?)

Connection flags (accepted on every subcommand):
  --server            host:port               (env MACHINERY_SERVER, default localhost:50051)
  --insecure          plaintext gRPC          (env MACHINERY_INSECURE, default true)
  --ca-cert           PEM CA bundle for TLS   (env MACHINERY_CA_CERT)
  --tls-skip-verify   skip TLS verify (dev)   (env MACHINERY_TLS_SKIP_VERIFY)
  --token             bearer token            (env MACHINERY_AUTH_TOKEN)
  --timeout           per-RPC timeout         (default 10s)
  --json              emit JSON, not a table

Run "consumer <command> --help" for command-specific flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "list":
		os.Exit(runList(os.Args[2:]))
	case "get":
		os.Exit(runGet(os.Args[2:]))
	case "health":
		os.Exit(runHealth(os.Args[2:]))
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// commonFlags holds the connection plumbing every subcommand shares.
type commonFlags struct {
	server        string
	insecure      bool
	caCert        string
	tlsSkipVerify bool
	token         string
	timeout       time.Duration
	asJSON        bool
}

func registerCommon(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.server, "server", envOr("MACHINERY_SERVER", "localhost:50051"), "gRPC server address (host:port)")
	fs.BoolVar(&c.insecure, "insecure", envBool("MACHINERY_INSECURE", true), "use plaintext gRPC (no TLS)")
	fs.StringVar(&c.caCert, "ca-cert", os.Getenv("MACHINERY_CA_CERT"), "path to PEM CA bundle (TLS only)")
	fs.BoolVar(&c.tlsSkipVerify, "tls-skip-verify", envBool("MACHINERY_TLS_SKIP_VERIFY", false), "skip TLS verification (dev only)")
	fs.StringVar(&c.token, "token", os.Getenv("MACHINERY_AUTH_TOKEN"), "bearer token; sent as `authorization: Bearer <token>` (pairs with auth interceptor)")
	fs.DurationVar(&c.timeout, "timeout", 10*time.Second, "per-RPC timeout")
	fs.BoolVar(&c.asJSON, "json", false, "emit JSON instead of a human-readable table")
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	var c commonFlags
	registerCommon(fs, &c)
	kind := fs.String("kind", "*", "kind to fetch (* for every configured kind)")
	count := fs.Int("count", 5, "max resources to return (-1 = all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	conn, err := dial(c)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resp, err := resourceservice.NewResourceServiceClient(conn).GetResources(ctx, &resourceservice.ResourceRequest{
		Count: int32(*count),
		Kind:  *kind,
	})
	if err != nil {
		return fail(err)
	}

	if c.asJSON {
		return emitJSON(resp.Resources)
	}
	return emitTable(resp.Resources)
}

func runGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	var c commonFlags
	registerCommon(fs, &c)
	kind := fs.String("kind", "", "kind (required)")
	name := fs.String("name", "", "resource name (required)")
	namespace := fs.String("namespace", "", "namespace (omit for cluster-scoped resources)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *kind == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "--kind and --name are required")
		fs.Usage()
		return 2
	}

	conn, err := dial(c)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	r, err := resourceservice.NewResourceServiceClient(conn).GetResourceDetail(ctx, &resourceservice.ResourceDetailRequest{
		Kind:      *kind,
		Name:      *name,
		Namespace: *namespace,
	})
	if err != nil {
		return fail(err)
	}

	if c.asJSON {
		return emitJSON(r)
	}
	return emitDetail(r)
}

func runHealth(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	var c commonFlags
	registerCommon(fs, &c)
	service := fs.String("service", "", "gRPC service name to check (empty = overall server health)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	conn, err := dial(c)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: *service})
	if err != nil {
		return fail(err)
	}
	fmt.Println(resp.Status.String())
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		return 1
	}
	return 0
}

// dial wires up TLS + optional bearer-token auth and returns a connection.
func dial(c commonFlags) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	if c.insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsCfg := &tls.Config{}
		switch {
		case c.tlsSkipVerify:
			tlsCfg.InsecureSkipVerify = true
		case c.caCert != "":
			pem, err := os.ReadFile(c.caCert)
			if err != nil {
				return nil, fmt.Errorf("reading --ca-cert: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("--ca-cert: no certificates parsed")
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	}

	if c.token != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(bearerToken{token: c.token, insecure: c.insecure}))
	}

	return grpc.NewClient(c.server, opts...)
}

// bearerToken is the smallest grpc.PerRPCCredentials implementation that
// pairs with machinery's auth interceptor — `authorization: Bearer <token>`
// on every outgoing RPC. RequireTransportSecurity is flipped off when
// --insecure is set so the same struct works on plaintext (LAN/dev) and
// TLS dial paths.
type bearerToken struct {
	token    string
	insecure bool
}

func (b bearerToken) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearerToken) RequireTransportSecurity() bool { return !b.insecure }

func emitTable(rs []*resourceservice.ResourceStatus) int {
	if len(rs) == 0 {
		fmt.Println("(no resources)")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAMESPACE\tNAME\tREADY\tSTATUS\tCONNECTION")
	for _, r := range rs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Kind, dash(r.Namespace), r.Name, boolStr(r.Ready), trunc(r.StatusMessage, 60), trunc(r.ConnectionDetails, 60),
		)
	}
	return flush(w)
}

func emitDetail(r *resourceservice.ResourceStatus) int {
	fmt.Printf("Kind:        %s\n", r.Kind)
	fmt.Printf("Namespace:   %s\n", dash(r.Namespace))
	fmt.Printf("Name:        %s\n", r.Name)
	fmt.Printf("Ready:       %s\n", boolStr(r.Ready))
	fmt.Printf("Status:      %s\n", r.StatusMessage)
	fmt.Printf("Connection:  %s\n", r.ConnectionDetails)
	if len(r.InfoFields) > 0 {
		fmt.Println("Info:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for k, v := range r.InfoFields {
			fmt.Fprintf(w, "  %s\t%s\n", k, v)
		}
		return flush(w)
	}
	return 0
}

func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return 0
}

func flush(w *tabwriter.Writer) int {
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	return 0
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// fail prints a "code: message" line and returns a non-zero exit code.
// gRPC status errors are unwrapped so callers see e.g.
// "Unauthenticated: invalid token" rather than the long rpc-error string.
func fail(err error) int {
	if err == nil {
		return 0
	}
	var msg string
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		msg = "timeout"
	default:
		if st, ok := status.FromError(err); ok {
			msg = fmt.Sprintf("%s: %s", st.Code(), st.Message())
		} else {
			msg = err.Error()
		}
	}
	fmt.Fprintln(os.Stderr, "error:", msg)
	return 1
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return fallback
}
