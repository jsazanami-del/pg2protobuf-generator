package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
)

func TestLoadMissingUsesDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Options.Validate {
		t.Fatal("validate should default true")
	}
	if cfg.Proto.PackagePrefix != "db.v1" {
		t.Fatalf("package %q", cfg.Proto.PackagePrefix)
	}
}

func TestWriteFileRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	if err := config.WriteFile(p, config.InitTemplate(), false); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFile(p, config.InitTemplate(), false); err == nil {
		t.Fatal("expected overwrite error")
	}
	if err := config.WriteFile(p, config.InitTemplate(), true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty file")
	}
}
