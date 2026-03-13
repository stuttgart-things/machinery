package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		content := `{
			"port": 9090,
			"resources": {
				"TestKind": {
					"group": "test.example.com",
					"version": "v1",
					"resource": "testkinds"
				}
			}
		}`
		path := writeTestFile(t, "config.json", content)

		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != 9090 {
			t.Errorf("expected port 9090, got %d", cfg.Port)
		}
		if len(cfg.Resources) != 1 {
			t.Errorf("expected 1 resource, got %d", len(cfg.Resources))
		}
		rk, ok := cfg.Resources["TestKind"]
		if !ok {
			t.Fatal("expected TestKind in resources")
		}
		if rk.Group != "test.example.com" {
			t.Errorf("expected group test.example.com, got %s", rk.Group)
		}
	})

	t.Run("default port when zero", func(t *testing.T) {
		content := `{
			"resources": {
				"TestKind": {
					"group": "test.example.com",
					"version": "v1",
					"resource": "testkinds"
				}
			}
		}`
		path := writeTestFile(t, "config.json", content)

		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != 50051 {
			t.Errorf("expected default port 50051, got %d", cfg.Port)
		}
	})

	t.Run("empty resources error", func(t *testing.T) {
		content := `{"port": 50051, "resources": {}}`
		path := writeTestFile(t, "config.json", content)

		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected error for empty resources")
		}
	})

	t.Run("invalid JSON error", func(t *testing.T) {
		path := writeTestFile(t, "config.json", "not json")

		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("missing file error", func(t *testing.T) {
		_, err := loadConfig("/nonexistent/config.json")
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Port != 50051 {
		t.Errorf("expected port 50051, got %d", cfg.Port)
	}
	if len(cfg.Resources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(cfg.Resources))
	}
	for _, kind := range []string{"AnsibleRun", "VsphereVMAnsible", "ProxmoxVMAnsible"} {
		if _, ok := cfg.Resources[kind]; !ok {
			t.Errorf("expected %s in default resources", kind)
		}
	}
}

func TestResourceKindToGVR(t *testing.T) {
	rk := ResourceKind{
		Group:    "resources.stuttgart-things.com",
		Version:  "v1alpha1",
		Resource: "ansibleruns",
	}
	gvr := rk.toGVR()
	if gvr.Group != rk.Group || gvr.Version != rk.Version || gvr.Resource != rk.Resource {
		t.Errorf("toGVR() mismatch: got %v", gvr)
	}
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	return path
}
