"""
ConfigMaps for machinery

- envConfigMap: scalar env vars consumed via envFrom
- fileConfigMap: the machinery JSON config, mounted as /etc/machinery/config.json
"""

import labels

config = labels.config

envConfigMap = {
    apiVersion: "v1"
    kind: "ConfigMap"
    metadata: {
        name: "{}-config".format(config.name)
        namespace: config.namespace
        labels: labels.commonLabels
    }
    data: {
        LOG_LEVEL: "info"
        **config.extraEnvVars
    }
}

fileConfigMap = {
    apiVersion: "v1"
    kind: "ConfigMap"
    metadata: {
        name: "{}-config-file".format(config.name)
        namespace: config.namespace
        labels: labels.commonLabels
    }
    data: {
        "config.json": config.configJson
    }
} if config.configJson else None
