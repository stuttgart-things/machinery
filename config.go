package main

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceKind struct {
	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`
}

type Config struct {
	Port      int                     `json:"port"`
	Resources map[string]ResourceKind `json:"resources"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if cfg.Port == 0 {
		cfg.Port = 50051
	}

	if len(cfg.Resources) == 0 {
		return nil, fmt.Errorf("config must define at least one resource kind")
	}

	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Port: 50051,
		Resources: map[string]ResourceKind{
			"AnsibleRun": {
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "ansibleruns",
			},
			"VsphereVMAnsible": {
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "vspherevmansibles",
			},
			"ProxmoxVMAnsible": {
				Group:    "resources.stuttgart-things.com",
				Version:  "v1alpha1",
				Resource: "proxmoxvmansibles",
			},
		},
	}
}

func (rk ResourceKind) toGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    rk.Group,
		Version:  rk.Version,
		Resource: rk.Resource,
	}
}
