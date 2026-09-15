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

func usersSnap(cols ...catalog.Column) *catalog.Snapshot {
	return &catalog.Snapshot{
		Relations: []catalog.Relation{{
			Schema:  "public",
			Name:    "users",
			Columns: cols,
		}},
	}
}

func TestOmitGlobAndPerMessage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Fields.Omit = []string{"password", "*_hash"}
	cfg.Messages["public.users"] = config.MessageSpec{Omit: []string{"internal_notes"}}
	snap := usersSnap(
		catalog.Column{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
		catalog.Column{Name: "password", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true},
		catalog.Column{Name: "password_hash", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true},
		catalog.Column{Name: "internal_notes", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: false},
		catalog.Column{Name: "email", TypeOID: pgtype.TextOID, TypeName: "text", NotNull: true},
	)
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]mapping.Field{}
	for _, f := range got.Relations[0].Fields {
		byName[f.Column] = f
	}
	if _, ok := byName["id"]; !ok {
		t.Fatal("id should remain")
	}
	if _, ok := byName["email"]; !ok {
		t.Fatal("email should remain")
	}
	for _, gone := range []string{"password", "password_hash", "internal_notes"} {
		if _, ok := byName[gone]; ok {
			t.Fatalf("%s should be omitted", gone)
		}
	}
}

func TestExtraMixinAndPerMessage(t *testing.T) {
	cfg := config.Defaults()
	cfg.Fields.Extra = []config.ExtraField{{
		Name:      "etag",
		ProtoType: "string",
		Optional:  true,
	}}
	cfg.Messages["public.users"] = config.MessageSpec{
		Extra: []config.ExtraField{{
			Name:      "display_name",
			ProtoType: "string",
			Optional:  true,
			Validate:  config.Validate{MaxLen: intPtr(255)},
		}, {
			Name:      "meta",
			ProtoType: "google.protobuf.Struct",
			Optional:  true,
		}},
	}
	snap := usersSnap(
		catalog.Column{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
	)
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]mapping.Field{}
	for _, f := range got.Relations[0].Fields {
		byName[f.Column] = f
	}
	assertField(t, byName["etag"], "string", false, true, nil)
	if !byName["etag"].Extra {
		t.Fatal("etag should be extra")
	}
	assertField(t, byName["display_name"], "string", false, true, nil)
	if !contains(byName["display_name"].Validate, "(buf.validate.field).string.max_len = 255") {
		t.Fatalf("display_name validate: %v", byName["display_name"].Validate)
	}
	assertField(t, byName["meta"], "google.protobuf.Struct", false, true, nil)
	if !contains(byName["meta"].Imports, "google/protobuf/struct.proto") {
		t.Fatalf("meta imports: %v", byName["meta"].Imports)
	}
}

func TestExtraCollision(t *testing.T) {
	cfg := config.Defaults()
	cfg.Fields.Extra = []config.ExtraField{{Name: "id", ProtoType: "string"}}
	snap := usersSnap(
		catalog.Column{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
	)
	if _, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil); err == nil {
		t.Fatal("expected collision")
	}
}

func TestExtraRequiresImport(t *testing.T) {
	cfg := config.Defaults()
	cfg.Fields.Extra = []config.ExtraField{{Name: "addr", ProtoType: "geo.Address"}}
	snap := usersSnap(
		catalog.Column{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
	)
	if _, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil); err == nil {
		t.Fatal("expected import error")
	}
}

func TestExtraOptionalAndRepeatedRejected(t *testing.T) {
	cfg := config.Defaults()
	cfg.Fields.Extra = []config.ExtraField{{Name: "tags", ProtoType: "string", Optional: true, Repeated: true}}
	snap := usersSnap(
		catalog.Column{Name: "id", TypeOID: pgtype.Int8OID, TypeName: "int8", NotNull: true},
	)
	if _, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil); err == nil {
		t.Fatal("expected optional+repeated error")
	}
}

func TestExcludedEnumMapsToString(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tables.Exclude = []string{"order_status"}
	snap := &catalog.Snapshot{
		Enums: []catalog.Enum{
			{Schema: "public", Name: "order_status", OID: 99901, Labels: []string{"pending"}},
			{Schema: "public", Name: "user_role", OID: 99902, Labels: []string{"admin"}},
		},
		Relations: []catalog.Relation{{
			Schema: "public",
			Name:   "users",
			Columns: []catalog.Column{
				{Name: "status", TypeOID: 99901, TypeName: "order_status", TypeType: 'e', TypeSchema: "public", NotNull: true},
				{Name: "roles", TypeOID: 99911, TypeName: "_user_role", TypeType: 'a', ElemOID: 99902, ElemName: "user_role", ElemType: 'e', ElemSchema: "public"},
				{Name: "history", TypeOID: 99912, TypeName: "_order_status", TypeType: 'a', ElemOID: 99901, ElemName: "order_status", ElemType: 'e', ElemSchema: "public"},
			},
		}},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Enums) != 1 || got.Enums[0].Name != "user_role" {
		t.Fatalf("enums: %+v", got.Enums)
	}
	byName := map[string]mapping.Field{}
	for _, f := range got.Relations[0].Fields {
		byName[f.Column] = f
	}
	assertField(t, byName["status"], "string", false, false, nil)
	if byName["status"].PGType != "order_status" {
		t.Fatalf("status pg type: %s", byName["status"].PGType)
	}
	if contains(byName["status"].Imports, "public/order_status.proto") {
		t.Fatalf("excluded enum should not import proto: %v", byName["status"].Imports)
	}
	assertField(t, byName["history"], "string", true, false, nil)
	if contains(byName["history"].Imports, "public/order_status.proto") {
		t.Fatalf("excluded enum array should not import proto: %v", byName["history"].Imports)
	}
	assertField(t, byName["roles"], "UserRole", true, false, nil)
	if !contains(byName["roles"].Imports, "public/user_role.proto") {
		t.Fatalf("kept enum should import proto: %v", byName["roles"].Imports)
	}
}

func TestExcludedEnumQualifiedAndGlob(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tables.Exclude = []string{"app.*", "public.legacy_*"}
	snap := &catalog.Snapshot{
		Enums: []catalog.Enum{
			{Schema: "app", Name: "color", OID: 1, Labels: []string{"red"}},
			{Schema: "public", Name: "legacy_status", OID: 2, Labels: []string{"old"}},
			{Schema: "public", Name: "order_status", OID: 3, Labels: []string{"ok"}},
		},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Enums) != 1 || got.Enums[0].Key() != "public.order_status" {
		t.Fatalf("enums: %+v", got.Enums)
	}
}

func TestExcludedEnumOverrideStillWins(t *testing.T) {
	cfg := config.Defaults()
	cfg.Tables.Exclude = []string{"order_status"}
	cfg.Overrides.Columns["public.users.status"] = config.Override{ProtoType: "bytes"}
	snap := &catalog.Snapshot{
		Enums: []catalog.Enum{{Schema: "public", Name: "order_status", OID: 99901, Labels: []string{"pending"}}},
		Relations: []catalog.Relation{{
			Schema:  "public",
			Name:    "users",
			Columns: []catalog.Column{{Name: "status", TypeOID: 99901, TypeName: "order_status", TypeType: 'e', TypeSchema: "public", NotNull: true}},
		}},
	}
	got, err := mapping.Apply(snap, cfg, pgtype.NewMap(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, got.Relations[0].Fields[0], "bytes", false, false, nil)
}

func intPtr(n int) *int { return &n }

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
