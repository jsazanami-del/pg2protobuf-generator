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

func TestOutputPackageAndFile(t *testing.T) {
	cfg := config.Defaults()
	if cfg.OutputPackage("public") != "db.v1" {
		t.Fatalf("default package: %q", cfg.OutputPackage("public"))
	}
	if cfg.OutputFile("public", "users") != "db/v1/users.proto" {
		t.Fatalf("default file: %q", cfg.OutputFile("public", "users"))
	}
	cfg.Proto.Schemas["public"] = "yagish_data.v1"
	if cfg.OutputPackage("public") != "yagish_data.v1" {
		t.Fatalf("mapped package: %q", cfg.OutputPackage("public"))
	}
	if cfg.OutputFile("public", "users") != "yagish_data/v1/users.proto" {
		t.Fatalf("mapped file: %q", cfg.OutputFile("public", "users"))
	}
	if cfg.OutputPackage("app") != "db.v1" {
		t.Fatalf("unmapped: %q", cfg.OutputPackage("app"))
	}
	cfg.Proto.GoPackagePrefix = "github.com/example/app/gen/proto"
	if g := cfg.GoPackage("public"); g != "github.com/example/app/gen/proto/yagish_data/v1" {
		t.Fatalf("go_package: %q", g)
	}
	cfg.Proto.GoPackagePrefix = "github.com/example/app/gen/proto/yagish_data/v1"
	if g := cfg.GoPackage("public"); g != "github.com/example/app/gen/proto/yagish_data/v1" {
		t.Fatalf("go_package already suffixed: %q", g)
	}
}

func TestValidatePackage(t *testing.T) {
	if err := config.ValidatePackage("db.v1"); err != nil {
		t.Fatal(err)
	}
	if err := config.ValidatePackage("yagish_data.v1"); err != nil {
		t.Fatal(err)
	}
	if err := config.ValidatePackage("yagish_data.v1beta1"); err != nil {
		t.Fatal(err)
	}
	if err := config.ValidatePackage("yagish_data.v1.public"); err == nil {
		t.Fatal("schema after version should fail")
	}
	if err := config.ValidatePackage("Yagish.v1"); err == nil {
		t.Fatal("uppercase should fail")
	}
	cfg := config.Defaults()
	cfg.Proto.Schemas["public"] = "yagish"
	if err := cfg.ValidateProto(); err == nil {
		t.Fatal("alias without version should fail")
	}
}

func TestLoadProtoOut(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(p, []byte("proto:\n  out: ./gen/proto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proto.Out != "./gen/proto" {
		t.Fatalf("out %q", cfg.Proto.Out)
	}
}

func TestLoadProtoSchemas(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(p, []byte("proto:\n  schemas:\n    public: yagish_data.v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputPackage("public") != "yagish_data.v1" {
		t.Fatalf("loaded package: %q", cfg.OutputPackage("public"))
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
