[package]
name = "deploy-machinery"
version = "0.1.0"
description = "KCL module for deploying machinery gRPC server on Kubernetes"

[dependencies]
k8s = "1.31"

[profile]
entries = [
    "main.k"
]
