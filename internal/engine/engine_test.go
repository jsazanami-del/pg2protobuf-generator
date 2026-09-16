package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/catalog"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/engine"
)

func fixture() *catalog.Snapshot {
	return &catalog.Snapshot{
		Enums: []catalog.Enum{{
			Schema: "public",
			Name:   "order_status",
			OID:    99901,
			Labels: []string{"pending", "shipped"},
		}},
		Relations: []catalog.Relation{{
			Schema: "public",
			Name:   "users",
			Columns: []catalog.Column{
				{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
				{Name: "email", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true},
				{Name: "created_at", TypeOID: pgtype.TimestamptzOID, TypeName: "timestamptz", NotNull: true},
				{Name: "tags", TypeOID: pgtype.TextArrayOID, TypeName: "_text", TypeType: 'a', ElemOID: pgtype.TextOID, ElemName: "text"},
				{Name: "nickname", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: false},
				{Name: "status", TypeOID: 99901, TypeName: "order_status", TypeType: 'e', TypeSchema: "public", NotNull: true},
				{Name: "password", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true},
			},
		}},
	}
}

func TestGenerateGolden(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := `version: "1"
proto:
  package_prefix: "db.v1"
  go_package_prefix: "github.com/example/app/gen/proto"
options:
  jsonb_as_struct: false
  validate: true
fields:
  omit: ["password"]
  extra:
    - name: etag
      proto_type: string
      optional: true
messages:
  public.users:
    extra:
      - name: display_name
        proto_type: string
        optional: true
overrides:
  columns:
    "public.users.email":
      proto_type: "string"
      validate:
        email: true
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "proto"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Breaking) != 0 {
		t.Fatal(res.Breaking)
	}
	users := res.Files["db/v1/users.proto"]
	if users == "" {
		t.Fatalf("files: %v", keys(res.Files))
	}
	if !strings.Contains(users, "package db.v1;") {
		t.Fatalf("package:\n%s", users)
	}
	if !strings.Contains(users, `option go_package = "github.com/example/app/gen/proto/db/v1";`) {
		t.Fatalf("go_package:\n%s", users)
	}
	if !strings.Contains(users, `import "db/v1/order_status.proto"`) {
		t.Fatalf("enum import:\n%s", users)
	}
	if !strings.Contains(users, "message Users {") {
		t.Fatalf("users proto:\n%s", users)
	}
	if !strings.Contains(users, "optional string nickname = ") {
		t.Fatalf("optional nickname:\n%s", users)
	}
	if !strings.Contains(users, "repeated string tags") {
		t.Fatalf("tags:\n%s", users)
	}
	if !strings.Contains(users, "google.protobuf.Timestamp created_at") {
		t.Fatalf("ts:\n%s", users)
	}
	if !strings.Contains(users, "(buf.validate.field).string.email = true") {
		t.Fatalf("email:\n%s", users)
	}
	if strings.Contains(users, "password") {
		t.Fatalf("password should be omitted:\n%s", users)
	}
	if !strings.Contains(users, "optional string etag") {
		t.Fatalf("etag:\n%s", users)
	}
	if !strings.Contains(users, "optional string display_name") {
		t.Fatalf("display_name:\n%s", users)
	}
	if strings.Contains(users, "gt = 0") {
		t.Fatal("must not auto-add gt=0")
	}
	st := res.Files["db/v1/order_status.proto"]
	if !strings.Contains(st, "ORDER_STATUS_UNSPECIFIED = 0") {
		t.Fatalf("enum:\n%s", st)
	}
	if !strings.Contains(st, "ORDER_STATUS_PENDING = 1") {
		t.Fatalf("enum values:\n%s", st)
	}
}

func TestSchemaAliasRewritesPackageAndDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := `version: "1"
proto:
  package_prefix: "yagish_data.v1"
  go_package_prefix: "github.com/example/app/gen/proto"
  schemas:
    public: yagish_data.v1
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "proto"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Files["public/users.proto"]; ok {
		t.Fatal("PG schema directory should not be used")
	}
	users := res.Files["yagish_data/v1/users.proto"]
	if users == "" {
		t.Fatalf("files: %v", keys(res.Files))
	}
	for _, want := range []string{
		"package yagish_data.v1;",
		`option go_package = "github.com/example/app/gen/proto/yagish_data/v1";`,
		`import "yagish_data/v1/order_status.proto";`,
		"OrderStatus status",
	} {
		if !strings.Contains(users, want) {
			t.Fatalf("missing %q in\n%s", want, users)
		}
	}
	if _, ok := res.Files["yagish_data/v1/order_status.proto"]; !ok {
		t.Fatal("enum should be under the package directory")
	}
	if _, ok := res.Lock.Messages["public.users"]; !ok {
		t.Fatal("lock keys should keep the PostgreSQL schema")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestYAMLOutDirectory(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "gen", "proto")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := "version: \"1\"\nproto:\n  package_prefix: \"db.v1\"\n  out: " + strconv.Quote(out) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != out {
		t.Fatalf("resolved out %q want %q", res.Out, out)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "db", "v1", "users.proto")); err != nil {
		t.Fatal(err)
	}
}

func TestFlagOutOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yamlOut := filepath.Join(dir, "from-yaml")
	flagOut := filepath.Join(dir, "from-flag")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  out: "+strconv.Quote(yamlOut)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      flagOut,
		OutSet:   true,
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != flagOut {
		t.Fatalf("flag should win: %q", res.Out)
	}
}

func TestInvalidPackageRejected(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  package_prefix: yagish_data.v1.public\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := engine.Run(context.Background(), engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	})
	if err == nil {
		t.Fatal("expected package version suffix error")
	}
}

func TestExcludeEnumFallsBackToString(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := `version: "1"
proto:
  package_prefix: "db.v1"
tables:
  exclude: ["order_status"]
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "proto"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Files["db/v1/order_status.proto"]; ok {
		t.Fatal("excluded enum proto should not be generated")
	}
	users := res.Files["db/v1/users.proto"]
	if users == "" {
		t.Fatalf("files: %v", keys(res.Files))
	}
	if strings.Contains(users, "import \"db/v1/order_status.proto\"") {
		t.Fatalf("users should not import excluded enum:\n%s", users)
	}
	if !strings.Contains(users, "string status") {
		t.Fatalf("excluded enum column should be string:\n%s", users)
	}
	if strings.Contains(users, "OrderStatus") {
		t.Fatalf("users should not reference OrderStatus:\n%s", users)
	}
}

func TestExcludeEnumFlagAndLockTransition(t *testing.T) {
	dir := t.TempDir()
	opt := engine.Options{
		Config:   filepath.Join(dir, "missing.yaml"),
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "proto"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}

	opt.Exclude = []string{"order_status"}
	res2, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Breaking) == 0 {
		t.Fatal("first exclude of a locked enum should be breaking")
	}
	if _, ok := res2.Lock.Enums["public.order_status"]; ok {
		t.Fatal("excluded enum should be dropped from the next lock")
	}
	if err := engine.Write(opt, res2); err != nil {
		t.Fatal(err)
	}

	res3, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res3.Breaking) != 0 {
		t.Fatalf("second pass should be compatible: %v", res3.Breaking)
	}
}

func TestCheckIncompatible(t *testing.T) {
	dir := t.TempDir()
	opt := engine.Options{
		Config:   filepath.Join(dir, "missing.yaml"),
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "proto"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}
	snap := fixture()
	snap.Relations[0].Columns[0].TypeOID = pgtype.TextOID
	snap.Relations[0].Columns[0].TypeName = "text"
	opt.Snapshot = snap
	res2, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Breaking) == 0 {
		t.Fatal("expected incompatible type change")
	}
}

func TestBufYAMLSingleModuleOut(t *testing.T) {
	dir := t.TempDir()
	writeEngineBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n")
	yamlOut := filepath.Join(dir, "from-yaml")
	flagOut := filepath.Join(dir, "from-flag")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := "version: \"1\"\nproto:\n  package_prefix: \"db.v1\"\n  go_package_prefix: \"github.com/example/app/gen/proto\"\n  out: " + strconv.Quote(yamlOut) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      flagOut,
		OutSet:   true,
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(dir, "proto"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != want {
		t.Fatalf("buf module should win: got %q want %q", res.Out, want)
	}
	users := res.Files["db/v1/users.proto"]
	if !strings.Contains(users, "package db.v1;") {
		t.Fatalf("package:\n%s", users)
	}
	if !strings.Contains(users, `import "db/v1/order_status.proto"`) {
		t.Fatalf("import:\n%s", users)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "proto", "db", "v1", "users.proto")); err != nil {
		t.Fatal(err)
	}
}

func TestBufYAMLModuleSelection(t *testing.T) {
	dir := t.TempDir()
	writeEngineBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  package_prefix: db.v1\n  module: proto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	wantProto, err := filepath.Abs(filepath.Join(dir, "proto"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != wantProto {
		t.Fatalf("yaml module: got %q want %q", res.Out, wantProto)
	}

	opt.Module = "proto/api"
	opt.ModuleSet = true
	res, err = engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(dir, "proto", "api"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != want {
		t.Fatalf("flag module should win: got %q want %q", res.Out, want)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "proto", "api", "db", "v1", "users.proto")); err != nil {
		t.Fatal(err)
	}
}

func TestBufYAMLMultipleModulesRequireSelection(t *testing.T) {
	dir := t.TempDir()
	writeEngineBuf(t, dir, "version: v2\nmodules:\n  - path: proto\n  - path: proto/api\n")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  package_prefix: db.v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := engine.Run(context.Background(), engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	})
	if err == nil {
		t.Fatal("expected module selection error")
	}
	if !strings.Contains(err.Error(), "multiple modules") {
		t.Fatalf("err: %v", err)
	}
}

func TestBufYAMLDotModuleWritesPackageDir(t *testing.T) {
	dir := t.TempDir()
	writeEngineBuf(t, dir, "version: v2\n")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  package_prefix: db.v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Write(opt, res); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "db", "v1", "users.proto")); err != nil {
		t.Fatal(err)
	}
}

func TestNoBufYAMLFallsBackToOut(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "gen", "proto")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	yaml := "version: \"1\"\nproto:\n  package_prefix: \"db.v1\"\n  out: " + strconv.Quote(out) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Out:      filepath.Join(dir, "ignored"),
		OutSet:   true,
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	}
	res, err := engine.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != opt.Out {
		t.Fatalf("flag out should win without buf.yaml: %q", res.Out)
	}
}

func TestBufYAMLUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	writeEngineBuf(t, dir, "version: v1\n")
	cfgPath := filepath.Join(dir, ".pg2proto.yaml")
	if err := os.WriteFile(cfgPath, []byte("proto:\n  package_prefix: db.v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := engine.Run(context.Background(), engine.Options{
		Config:   cfgPath,
		LockFile: filepath.Join(dir, ".pg2proto.lock"),
		Snapshot: fixture(),
		Schemas:  []string{"public"},
	})
	if err == nil {
		t.Fatal("expected unsupported buf.yaml version")
	}
}

func writeEngineBuf(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "buf.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
