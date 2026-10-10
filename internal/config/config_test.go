package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.HTTPPort != 8000 {
		t.Fatalf("expected HTTPPort 8000, got %d", cfg.Server.HTTPPort)
	}
	if cfg.Server.RTSPPort != 8554 {
		t.Fatalf("expected RTSPPort 8554, got %d", cfg.Server.RTSPPort)
	}
	if cfg.Streams.Main.Width != 1920 || cfg.Streams.Main.Height != 1080 {
		t.Fatalf("expected 1920x1080 main stream, got %dx%d", cfg.Streams.Main.Width, cfg.Streams.Main.Height)
	}
	if cfg.Streams.Sub.Width != 640 || cfg.Streams.Sub.Height != 360 {
		t.Fatalf("expected 640x360 sub stream, got %dx%d", cfg.Streams.Sub.Width, cfg.Streams.Sub.Height)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test_config.yaml")

	cfg := DefaultConfig()
	cfg.Server.HTTPPort = 9000
	cfg.Camera.Model = "Custom-Model"

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Server.HTTPPort != 9000 {
		t.Errorf("expected loaded HTTPPort 9000, got %d", loaded.Server.HTTPPort)
	}
	if loaded.Camera.Model != "Custom-Model" {
		t.Errorf("expected loaded Model 'Custom-Model', got %q", loaded.Camera.Model)
	}
}

func TestGenerateInitConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "init_config.yaml")

	if err := GenerateInitConfig(path); err != nil {
		t.Fatalf("GenerateInitConfig failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("Init config file not created or empty")
	}
}
