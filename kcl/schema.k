"""
Schema definition for machinery deployment configuration
"""

schema Machinery:
    """Configuration for machinery Kubernetes deployment"""

    # Metadata
    name: str = "machinery"
    namespace: str = "machinery"

    # Image
    image: str = "ghcr.io/stuttgart-things/machinery:latest"
    imagePullPolicy: str = "IfNotPresent"

    # Replicas and resources
    replicas: int = 1
    cpuRequest: str = "100m"
    cpuLimit: str = "500m"
    memoryRequest: str = "128Mi"
    memoryLimit: str = "512Mi"

    # gRPC server
    grpcPort: int = 50051

    # HTTP server (HTMX frontend)
    httpPort: int = 8080
    httpEnabled: bool = False

    # Service
    serviceType: str = "ClusterIP"

    # HTTPRoute (Gateway API)
    httpRouteEnabled: bool = False
    httpRouteParentRefName: str = ""
    httpRouteParentRefNamespace: str = ""
    httpRouteHostname: str = ""
    httpRouteAnnotations: {str:str} = {}

    # Kubeconfig
    kubeconfigSecret: str = ""

    # Config file
    configJson: str = ""

    # Extra environment variables
    extraEnvVars: {str:str} = {}

    # Labels and annotations
    labels: {str:str} = {}
    annotations: {str:str} = {}

    check:
        replicas >= 0, "replicas must be >= 0"
        grpcPort >= 1 and grpcPort <= 65535, "grpcPort must be valid"
        httpPort >= 1 and httpPort <= 65535, "httpPort must be valid"
        name != "", "name must not be empty"
        namespace != "", "namespace must not be empty"
