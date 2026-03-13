"""
Common labels for machinery resources
"""

import schema

# Get command-line options
_image = option("config.image")
_namespace = option("config.namespace")
_grpcPort = option("config.grpcPort")
_httpPort = option("config.httpPort")
_httpEnabled = option("config.httpEnabled")
_kubeconfigSecret = option("config.kubeconfigSecret")
_configJson = option("config.configJson")
_httpRouteEnabled = option("config.httpRouteEnabled")
_httpRouteParentRefName = option("config.httpRouteParentRefName")
_httpRouteParentRefNamespace = option("config.httpRouteParentRefNamespace")
_httpRouteHostname = option("config.httpRouteHostname")
_httpRouteAnnotations = option("config.httpRouteAnnotations") or {}
_extraEnvVars = option("config.extraEnvVars") or {}

# Config instance with command-line overrides
config: schema.Machinery = schema.Machinery {
    if _image:
        image = _image
    if _namespace:
        namespace = _namespace
    if _grpcPort:
        grpcPort = int(_grpcPort)
    if _httpPort:
        httpPort = int(_httpPort)
    if _httpEnabled:
        httpEnabled = _httpEnabled in [True, "True", "true"]
    if _kubeconfigSecret:
        kubeconfigSecret = _kubeconfigSecret
    if _configJson:
        configJson = _configJson
    if _httpRouteEnabled:
        httpRouteEnabled = _httpRouteEnabled in [True, "True", "true"]
    if _httpRouteParentRefName:
        httpRouteParentRefName = _httpRouteParentRefName
    if _httpRouteParentRefNamespace:
        httpRouteParentRefNamespace = _httpRouteParentRefNamespace
    if _httpRouteHostname:
        httpRouteHostname = _httpRouteHostname
    if _httpRouteAnnotations:
        httpRouteAnnotations = _httpRouteAnnotations
    if _extraEnvVars:
        extraEnvVars = _extraEnvVars
}

# Common labels applied to all resources
commonLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
    "app.kubernetes.io/component": "grpc-server"
    "app.kubernetes.io/part-of": "stuttgart-things"
    **config.labels
}

# Selector labels for pods
selectorLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
}
