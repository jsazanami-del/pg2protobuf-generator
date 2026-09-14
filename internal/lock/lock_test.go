package lock_test

import (
	"testing"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/lock"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/mapping"
)

func TestAssignStableNumbers(t *testing.T) {
	cfg := config.Defaults()
	mapped := &mapping.Schema{
		Relations: []mapping.Relation{{
			Schema:    "public",
			Name:      "users",
			ProtoName: "Users",
			Fields: []mapping.Field{
				{Column: "id", ProtoName: "id", ProtoType: "int64", PGType: "int8"},
				{Column: "email", ProtoName: "email", ProtoType: "string", PGType: "text"},
			},
		}},
	}
	r1 := lock.Assign(lock.Empty(), mapped, cfg)
	if r1.Lock.Messages["public.users"].Fields["id"].Number != 1 {
		t.Fatal(r1.Lock.Messages["public.users"].Fields)
	}
	mapped.Relations[0].Fields = []mapping.Field{
		{Column: "email", ProtoName: "email", ProtoType: "string", PGType: "text"},
		{Column: "id", ProtoName: "id", ProtoType: "int64", PGType: "int8"},
		{Column: "age", ProtoName: "age", ProtoType: "int32", PGType: "int4"},
	}
	r2 := lock.Assign(r1.Lock, mapped, cfg)
	if r2.Lock.Messages["public.users"].Fields["id"].Number != 1 {
		t.Fatalf("id number changed")
	}
	if r2.Lock.Messages["public.users"].Fields["age"].Number != 3 {
		t.Fatalf("age = %d", r2.Lock.Messages["public.users"].Fields["age"].Number)
	}
	if len(r2.Breaking) != 0 {
		t.Fatal(r2.Breaking)
	}
}

func TestReservedOnDrop(t *testing.T) {
	cfg := config.Defaults()
	mapped := &mapping.Schema{
		Relations: []mapping.Relation{{
			Schema:    "public",
			Name:      "users",
			ProtoName: "Users",
			Fields: []mapping.Field{
				{Column: "id", ProtoName: "id", ProtoType: "int64", PGType: "int8"},
				{Column: "gone", ProtoName: "gone", ProtoType: "string", PGType: "text"},
			},
		}},
	}
	r1 := lock.Assign(lock.Empty(), mapped, cfg)
	mapped.Relations[0].Fields = []mapping.Field{
		{Column: "id", ProtoName: "id", ProtoType: "int64", PGType: "int8"},
	}
	r2 := lock.Assign(r1.Lock, mapped, cfg)
	if len(r2.Lock.Messages["public.users"].ReservedNumbers) == 0 {
		t.Fatal("expected reserved")
	}
	if len(r2.Breaking) != 0 {
		t.Fatalf("drop-only should be compatible: %v", r2.Breaking)
	}
}

func TestUndeclaredRenameBreaks(t *testing.T) {
	cfg := config.Defaults()
	mapped := &mapping.Schema{
		Relations: []mapping.Relation{{
			Schema:    "public",
			Name:      "users",
			ProtoName: "Users",
			Fields: []mapping.Field{
				{Column: "old_email", ProtoName: "old_email", ProtoType: "string", PGType: "text"},
			},
		}},
	}
	r1 := lock.Assign(lock.Empty(), mapped, cfg)
	mapped.Relations[0].Fields = []mapping.Field{
		{Column: "email", ProtoName: "email", ProtoType: "string", PGType: "text"},
	}
	r2 := lock.Assign(r1.Lock, mapped, cfg)
	if len(r2.Breaking) == 0 {
		t.Fatal("expected breaking rename")
	}
	cfg.Renames.Columns = map[string]string{"public.users.old_email": "email"}
	r3 := lock.Assign(r1.Lock, mapped, cfg)
	if len(r3.Breaking) != 0 {
		t.Fatal(r3.Breaking)
	}
	if r3.Lock.Messages["public.users"].Fields["email"].Number != 1 {
		t.Fatal("rename should keep number")
	}
}

func TestTypeChangeBreaks(t *testing.T) {
	cfg := config.Defaults()
	mapped := &mapping.Schema{
		Relations: []mapping.Relation{{
			Schema:    "public",
			Name:      "users",
			ProtoName: "Users",
			Fields: []mapping.Field{
				{Column: "id", ProtoName: "id", ProtoType: "int32", PGType: "int4"},
			},
		}},
	}
	r1 := lock.Assign(lock.Empty(), mapped, cfg)
	mapped.Relations[0].Fields[0].ProtoType = "string"
	r2 := lock.Assign(r1.Lock, mapped, cfg)
	if len(r2.Breaking) == 0 {
		t.Fatal("expected type change")
	}
}

func TestSkipProtobufInternalRange(t *testing.T) {
	used := map[int]bool{}
	for i := 1; i < 19000; i++ {
		used[i] = true
	}
	// nextNumber is not exported; simulate via huge reserved
	cfg := config.Defaults()
	fields := make([]mapping.Field, 0, 3)
	old := lock.Empty()
	old.Messages["public.t"] = &lock.Message{
		ProtoName:       "T",
		Fields:          map[string]*lock.Field{},
		ReservedNumbers: []int{},
	}
	for n := 1; n <= 18999; n++ {
		old.Messages["public.t"].ReservedNumbers = append(old.Messages["public.t"].ReservedNumbers, n)
	}
	mapped := &mapping.Schema{
		Relations: []mapping.Relation{{
			Schema:    "public",
			Name:      "t",
			ProtoName: "T",
			Fields:    append(fields, mapping.Field{Column: "x", ProtoName: "x", ProtoType: "int32", PGType: "int4"}),
		}},
	}
	r := lock.Assign(old, mapped, cfg)
	if r.Lock.Messages["public.t"].Fields["x"].Number != 20000 {
		t.Fatalf("got %d", r.Lock.Messages["public.t"].Fields["x"].Number)
	}
}
