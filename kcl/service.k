"""
Service for machinery
"""

import labels

config = labels.config

service = {
    apiVersion: "v1"
    kind: "Service"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
    spec: {
        type: config.serviceType
        ports: [
            {
                name: "grpc"
                port: config.grpcPort
                targetPort: "grpc"
                protocol: "TCP"
            }
        ] + (if config.httpEnabled: [
            {
                name: "http"
                port: config.httpPort
                targetPort: "http"
                protocol: "TCP"
            }
        ] else [])
        selector: labels.selectorLabels
    }
}
