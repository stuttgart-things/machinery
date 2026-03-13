"""
Deployment for machinery
"""

import labels

config = labels.config

deployment = {
    apiVersion: "apps/v1"
    kind: "Deployment"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
    spec: {
        replicas: config.replicas
        selector: {
            matchLabels: labels.selectorLabels
        }
        strategy: {
            type: "RollingUpdate"
            rollingUpdate: {
                maxSurge: 1
                maxUnavailable: 0
            }
        }
        template: {
            metadata: {
                labels: labels.commonLabels
            }
            spec: {
                serviceAccountName: config.name
                containers: [
                    {
                        name: config.name
                        image: config.image
                        imagePullPolicy: config.imagePullPolicy
                        ports: [
                            {
                                name: "grpc"
                                containerPort: config.grpcPort
                                protocol: "TCP"
                            }
                        ] + (if config.httpEnabled: [
                            {
                                name: "http"
                                containerPort: config.httpPort
                                protocol: "TCP"
                            }
                        ] else [])
                        env: [
                            if config.kubeconfigSecret:
                                {
                                    name: "KUBECONFIG"
                                    value: "/etc/kubeconfig/config"
                                }
                            if config.configJson:
                                {
                                    name: "MACHINERY_CONFIG"
                                    valueFrom: {
                                        configMapKeyRef: {
                                            name: "{}-config".format(config.name)
                                            key: "config.json"
                                        }
                                    }
                                }
                        ]
                        envFrom: [
                            {
                                configMapRef: {
                                    name: "{}-config".format(config.name)
                                }
                            }
                        ]
                        resources: {
                            requests: {
                                cpu: config.cpuRequest
                                memory: config.memoryRequest
                            }
                            limits: {
                                cpu: config.cpuLimit
                                memory: config.memoryLimit
                            }
                        }
                        livenessProbe: {
                            grpc: {
                                port: config.grpcPort
                            }
                            initialDelaySeconds: 10
                            periodSeconds: 10
                            timeoutSeconds: 5
                            failureThreshold: 3
                        }
                        readinessProbe: {
                            grpc: {
                                port: config.grpcPort
                            }
                            initialDelaySeconds: 5
                            periodSeconds: 5
                            timeoutSeconds: 3
                            failureThreshold: 3
                        }
                        securityContext: {
                            readOnlyRootFilesystem: True
                            runAsNonRoot: True
                            allowPrivilegeEscalation: False
                            capabilities: {
                                drop: ["ALL"]
                            }
                        }
                        volumeMounts: [
                            {
                                name: "tmp"
                                mountPath: "/tmp"
                            }
                        ] + (if config.kubeconfigSecret: [
                            {
                                name: "kubeconfig"
                                mountPath: "/etc/kubeconfig"
                                readOnly: True
                            }
                        ] else [])
                    }
                ]
                securityContext: {
                    runAsNonRoot: True
                    runAsUser: 65532
                    runAsGroup: 65532
                    fsGroup: 65532
                    seccompProfile: {
                        type: "RuntimeDefault"
                    }
                }
                volumes: [
                    {
                        name: "tmp"
                        emptyDir: {}
                    }
                ] + (if config.kubeconfigSecret: [
                    {
                        name: "kubeconfig"
                        secret: {
                            secretName: config.kubeconfigSecret
                        }
                    }
                ] else [])
                affinity: {
                    podAntiAffinity: {
                        preferredDuringSchedulingIgnoredDuringExecution: [
                            {
                                weight: 100
                                podAffinityTerm: {
                                    labelSelector: {
                                        matchLabels: labels.selectorLabels
                                    }
                                    topologyKey: "kubernetes.io/hostname"
                                }
                            }
                        ]
                    }
                }
            }
        }
    }
}
