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

func TestLoadFieldsAndMessages(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	body := `version: "1"
fields:
  omit: ["password", "*_hash"]
  extra:
    - name: etag
      proto_type: string
      optional: true
messages:
  public.users:
    omit: ["internal_notes"]
    extra:
      - name: display_name
        proto_type: string
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OmitsColumn("public", "users", "password") {
		t.Fatal("password should be omitted")
	}
	if !cfg.OmitsColumn("public", "users", "password_hash") {
		t.Fatal("password_hash should be omitted")
	}
	if !cfg.OmitsColumn("public", "users", "internal_notes") {
		t.Fatal("internal_notes should be omitted")
	}
	if cfg.OmitsColumn("public", "users", "email") {
		t.Fatal("email should remain")
	}
	if len(cfg.Fields.Extra) != 1 || cfg.Fields.Extra[0].Name != "etag" {
		t.Fatalf("extra: %+v", cfg.Fields.Extra)
	}
	if cfg.Messages["public.users"].Extra[0].Name != "display_name" {
		t.Fatalf("messages extra: %+v", cfg.Messages["public.users"])
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
