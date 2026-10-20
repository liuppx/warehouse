package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadConfigDefaultsToConfigYAML(t *testing.T) {
	workingDir := t.TempDir()
	configPath := filepath.Join(workingDir, "config.yaml")
	contents := "server:\n  port: 7070\nwebdav:\n  directory: \"" + filepath.Join(workingDir, "data") + "\"\nweb3:\n  jwt_secret: \"0123456789abcdef0123456789abcdef\"\n"
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	cfg, err := loadConfig("", flags)
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.Server.Port != 7070 {
		t.Fatalf("expected config.yaml server port 7070, got %d", cfg.Server.Port)
	}
}
