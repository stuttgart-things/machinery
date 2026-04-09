package main

import (
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type InfoField struct {
	Label string `json:"label"`
	Path  string `json:"path"` // dot-separated path, e.g. "spec.vm.name"
}

type ResourceKind struct {
	Group           string      `json:"group"`
	Version         string      `json:"version"`
	Resource        string      `json:"resource"`
	ConnectionField string      `json:"connectionField,omitempty"` // dot-separated path, e.g. "status.share.ips"
	StatusFields    []string    `json:"statusFields,omitempty"`    // extra status fields to display
	InfoFields      []InfoField `json:"infoFields,omitempty"`      // additional fields for detail view
}

type Config struct {
	Port     int                     `json:"port"`
	HttpPort int                     `json:"httpPort"`
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
	if cfg.HttpPort == 0 {
		cfg.HttpPort = 8080
	}

	if len(cfg.Resources) == 0 {
		return nil, fmt.Errorf("config must define at least one resource kind")
	}

	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Port:     50051,
		HttpPort: 8080,
		Resources: map[string]ResourceKind{
			"HarvesterVM": {
				Group:           "resources.stuttgart-things.com",
				Version:         "v1alpha1",
				Resource:        "harvestervms",
				ConnectionField: "status.vm.name",
				StatusFields:    []string{"status.volume.ready", "status.cloudInit.ready", "status.vm.ready"},
			},
			"StoragePlatform": {
				Group:           "resources.stuttgart-things.com",
				Version:         "v1alpha1",
				Resource:        "storageplatforms",
				StatusFields:    []string{"status.installed", "status.observedVersion"},
			},
			"NetworkIntegration": {
				Group:           "resources.stuttgart-things.com",
				Version:         "v1alpha1",
				Resource:        "networkintegrations",
				StatusFields:    []string{"status.installed", "status.observedVersion"},
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
