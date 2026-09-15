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

func TestOutputSchemaAlias(t *testing.T) {
	cfg := config.Defaults()
	if cfg.OutputSchema("public") != "public" {
		t.Fatalf("default alias: %q", cfg.OutputSchema("public"))
	}
	cfg.Proto.Schemas["public"] = "yagish"
	if cfg.OutputSchema("public") != "yagish" {
		t.Fatalf("aliased: %q", cfg.OutputSchema("public"))
	}
	if cfg.OutputSchema("app") != "app" {
		t.Fatalf("unmapped: %q", cfg.OutputSchema("app"))
	}
	cfg.Proto.Schemas["app"] = ""
	if cfg.OutputSchema("app") != "app" {
		t.Fatalf("empty alias should keep PG name: %q", cfg.OutputSchema("app"))
	}
}

func TestLoadProtoSchemas(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(p, []byte("proto:\n  schemas:\n    public: yagish\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputSchema("public") != "yagish" {
		t.Fatalf("loaded alias: %q", cfg.OutputSchema("public"))
	}
}

func TestExcludesObjectAndSelectsRelation(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tables.Exclude = []string{"_*", "order_status", "app.hidden"}
	if !cfg.ExcludesObject("public", "order_status") {
		t.Fatal("unqualified exclude should match enum/relation name")
	}
	if !cfg.ExcludesObject("app", "hidden") {
		t.Fatal("qualified exclude should match schema.name")
	}
	if cfg.ExcludesObject("public", "users") {
		t.Fatal("users should remain")
	}
	if !cfg.ExcludesObject("public", "_tmp") {
		t.Fatal("_* should match _tmp")
	}
	if !cfg.SelectsRelation("public", "users") {
		t.Fatal("users should be selected")
	}
	if cfg.SelectsRelation("public", "_tmp") {
		t.Fatal("_tmp should be excluded")
	}
	cfg.Tables.Include = []string{"users"}
	if !cfg.SelectsRelation("public", "users") {
		t.Fatal("include should keep users")
	}
	if cfg.SelectsRelation("public", "orders") {
		t.Fatal("include should drop unmatched relations")
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
