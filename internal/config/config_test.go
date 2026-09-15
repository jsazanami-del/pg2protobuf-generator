package config_test

import (
	"os"
	"path/filepath"
	"strings"
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
	if !strings.Contains(string(b), "module:") {
		t.Fatal("init template should mention proto.module")
	}
}

func TestLoadProtoModule(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(p, []byte("proto:\n  module: proto/api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proto.Module != "proto/api" {
		t.Fatalf("module %q", cfg.Proto.Module)
	}
}

func TestResolveBufModule(t *testing.T) {
	abs := func(t *testing.T, p string) string {
		t.Helper()
		out, err := filepath.Abs(p)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("single module", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n")
		got, ok, err := config.ResolveBufModule(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected buf.yaml")
		}
		if got != abs(t, filepath.Join(dir, "proto")) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("omitted modules is dot", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\n")
		got, ok, err := config.ResolveBufModule(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected buf.yaml")
		}
		if got != abs(t, dir) {
			t.Fatalf("got %q want %q", got, abs(t, dir))
		}
	})

	t.Run("walks up from nested config dir", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n")
		nested := filepath.Join(dir, "app", "cfg")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		got, ok, err := config.ResolveBufModule(nested, "")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected ancestor buf.yaml")
		}
		if got != abs(t, filepath.Join(dir, "proto")) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("multiple requires selection", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
		_, _, err := config.ResolveBufModule(dir, "")
		if err == nil {
			t.Fatal("expected multiple-module error")
		}
		if !strings.Contains(err.Error(), "proto") || !strings.Contains(err.Error(), "proto/api") {
			t.Fatalf("candidates: %v", err)
		}
	})

	t.Run("selects matching path", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
		got, ok, err := config.ResolveBufModule(dir, "proto/api")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected buf.yaml")
		}
		if got != abs(t, filepath.Join(dir, "proto", "api")) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("normalizes selected path", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
		got, _, err := config.ResolveBufModule(dir, "./proto/")
		if err != nil {
			t.Fatal(err)
		}
		if got != abs(t, filepath.Join(dir, "proto")) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("unknown selection lists candidates", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
		_, _, err := config.ResolveBufModule(dir, "missing")
		if err == nil {
			t.Fatal("expected missing module error")
		}
		if !strings.Contains(err.Error(), `matching "missing"`) {
			t.Fatalf("err: %v", err)
		}
	})

	t.Run("explicit dot module", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: .\n  - path: proto\n")
		got, _, err := config.ResolveBufModule(dir, ".")
		if err != nil {
			t.Fatal(err)
		}
		if got != abs(t, dir) {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v1\nbuild:\n  roots:\n    - proto\n")
		_, _, err := config.ResolveBufModule(dir, "")
		if err == nil {
			t.Fatal("expected version error")
		}
		if !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("err: %v", err)
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: [\n")
		_, _, err := config.ResolveBufModule(dir, "")
		if err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("empty modules list", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules: []\n")
		_, _, err := config.ResolveBufModule(dir, "")
		if err == nil {
			t.Fatal("expected no modules error")
		}
	})

	t.Run("duplicate module path", func(t *testing.T) {
		dir := t.TempDir()
		writeBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: ./proto\n")
		_, _, err := config.ResolveBufModule(dir, "proto")
		if err == nil {
			t.Fatal("expected duplicate error")
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		got, ok, err := config.ResolveBufModule(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		if ok || got != "" {
			t.Fatalf("got %q ok=%v", got, ok)
		}
	})
}

func writeBuf(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "buf.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
