"""
Main entry point for machinery Kubernetes deployment
Imports all resources and exports manifests
"""

import namespace
import serviceaccount
import configmap
import deploy
import service
import httproute

# Export all manifests (filter out None values)
manifests = [
    m for m in [
        namespace.namespace
        serviceaccount.serviceAccount
        configmap.configMap
        deploy.deployment
        service.service
        httproute.httproute
    ] if m
]
