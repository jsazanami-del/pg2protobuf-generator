package mapping_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/catalog"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/mapping"
)

func TestMapBasicTypes(t *testing.T) {
	cfg := config.Defaults()
	snap := &catalog.Snapshot{
		Relations: []catalog.Relation{{
			Schema: "public",
			Name:   "users",
			Columns: []catalog.Column{
				{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
				{Name: "email", TypeOID: pgtype.VarcharOID, TypeName: "varchar", TypeMod: 255 + 4, NotNull: true},
				{Name: "uid", TypeOID: pgtype.UUIDOID, TypeName: "uuid", NotNull: true},
				{Name: "created_at", TypeOID: pgtype.TimestamptzOID, TypeName: "timestamptz", NotNull: true},
				{Name: "nickname", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: false},
				{Name: "tags", TypeOID: pgtype.TextArrayOID, TypeName: "_text", TypeType: 'a', ElemOID: pgtype.TextOID, ElemName: "text", NotNull: false},
				{Name: "blob", TypeOID: pgtype.ByteaOID, TypeName: "bytea", NotNull: true},
				{Name: "amount", TypeOID: pgtype.NumericOID, TypeName: "numeric", NotNull: true},
			},
		}},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Relations) != 1 {
		t.Fatal(got.Relations)
	}
	byName := map[string]mapping.Field{}
	for _, f := range got.Relations[0].Fields {
		byName[f.Column] = f
	}
	assertField(t, byName["id"], "int64", false, false, nil)
	assertField(t, byName["nickname"], "string", false, true, nil)
	assertField(t, byName["tags"], "string", true, false, nil)
	assertField(t, byName["blob"], "bytes", false, false, nil)
	assertField(t, byName["amount"], "string", false, false, nil)
	if byName["created_at"].ProtoType != "google.protobuf.Timestamp" {
		t.Fatalf("created_at %s", byName["created_at"].ProtoType)
	}
	if !contains(byName["uid"].Validate, "(buf.validate.field).string.uuid = true") {
		t.Fatalf("uuid validate: %v", byName["uid"].Validate)
	}
	if !contains(byName["email"].Validate, "(buf.validate.field).string.max_len = 255") {
		t.Fatalf("email validate: %v", byName["email"].Validate)
	}
}

func TestUnknownTypeErrors(t *testing.T) {
	cfg := config.Defaults()
	cfg.Options.StrictTypes = true
	snap := &catalog.Snapshot{
		Relations: []catalog.Relation{{
			Schema:  "public",
			Name:    "t",
			Columns: []catalog.Column{{Name: "c", TypeOID: 999999, TypeName: "mystery", NotNull: true}},
		}},
	}
	if _, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnknownTypeAsAny(t *testing.T) {
	cfg := config.Defaults()
	snap := &catalog.Snapshot{
		Relations: []catalog.Relation{{
			Schema: "public",
			Name:   "t",
			Columns: []catalog.Column{
				{Name: "c", TypeOID: 999999, TypeName: "mystery", NotNull: true},
				{Name: "rec", TypeOID: pgtype.RecordOID, TypeName: "record", NotNull: false},
			},
		}},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := got.Relations[0].Fields[0]
	if c.ProtoType != "google.protobuf.Any" {
		t.Fatalf("mystery -> %s", c.ProtoType)
	}
	if !contains(c.Imports, "google/protobuf/any.proto") {
		t.Fatalf("imports %v", c.Imports)
	}
	rec := got.Relations[0].Fields[1]
	if rec.ProtoType != "google.protobuf.Any" || !rec.Optional {
		t.Fatalf("record %+v", rec)
	}
}

func TestColumnOverrideEmail(t *testing.T) {
	cfg := config.Defaults()
	tru := true
	cfg.Overrides.Columns["public.users.email"] = config.Override{
		ProtoType: "string",
		Validate:  config.Validate{Email: &tru},
	}
	snap := &catalog.Snapshot{
		Relations: []catalog.Relation{{
			Schema:  "public",
			Name:    "users",
			Columns: []catalog.Column{{Name: "email", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true}},
		}},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := got.Relations[0].Fields[0]
	if !contains(f.Validate, "(buf.validate.field).string.email = true") {
		t.Fatalf("%v", f.Validate)
	}
}

func assertField(t *testing.T, f mapping.Field, proto string, repeated, optional bool, _ []string) {
	t.Helper()
	if f.ProtoType != proto || f.Repeated != repeated || f.Optional != optional {
		t.Fatalf("%s: type=%s repeated=%v optional=%v", f.Column, f.ProtoType, f.Repeated, f.Optional)
	}
}

func contains(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
