package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Load reads tables/views/matviews and enums from PostgreSQL catalogs.
func Load(ctx context.Context, conn *pgx.Conn, schemas []string) (*Snapshot, error) {
	if len(schemas) == 0 {
		return nil, fmt.Errorf("no schemas specified")
	}
	snap := &Snapshot{}
	if err := loadRelations(ctx, conn, schemas, snap); err != nil {
		return nil, err
	}
	if err := loadColumns(ctx, conn, schemas, snap); err != nil {
		return nil, err
	}
	if err := loadEnums(ctx, conn, schemas, snap); err != nil {
		return nil, err
	}
	return snap, nil
}

func loadRelations(ctx context.Context, conn *pgx.Conn, schemas []string, snap *Snapshot) error {
	const q = `
SELECT n.nspname, c.relname, c.relkind::text, COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = ANY($1)
  AND c.relkind IN ('r', 'v', 'm')
  AND NOT c.relispartition
ORDER BY n.nspname, c.relname`
	rows, err := conn.Query(ctx, q, schemas)
	if err != nil {
		return fmt.Errorf("list relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.Schema, &r.Name, &r.Kind, &r.Comment); err != nil {
			return err
		}
		snap.Relations = append(snap.Relations, r)
	}
	return rows.Err()
}

func loadColumns(ctx context.Context, conn *pgx.Conn, schemas []string, snap *Snapshot) error {
	const q = `
SELECT
  n.nspname,
  c.relname,
  a.attname,
  a.atttypid,
  a.atttypmod,
  a.attnotnull,
  t.typname,
  t.typtype::text,
  t.typelem,
  COALESCE(tn.nspname, ''),
  COALESCE(et.typname, ''),
  COALESCE(et.typtype::text, ''),
  COALESCE(en.nspname, ''),
  COALESCE(col_description(c.oid, a.attnum), '')
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_type t ON t.oid = a.atttypid
JOIN pg_namespace tn ON tn.oid = t.typnamespace
LEFT JOIN pg_type et ON et.oid = t.typelem
LEFT JOIN pg_namespace en ON en.oid = et.typnamespace
WHERE n.nspname = ANY($1)
  AND c.relkind IN ('r', 'v', 'm')
  AND NOT c.relispartition
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY n.nspname, c.relname, a.attnum`
	rows, err := conn.Query(ctx, q, schemas)
	if err != nil {
		return fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()

	idx := map[string]int{}
	for i, r := range snap.Relations {
		idx[r.Key()] = i
	}
	for rows.Next() {
		var schema, rel, name, typname, typtype, typeSchema, elemName, elemType, elemSchema, comment string
		var oid, elemOID uint32
		var typmod int32
		var notNull bool
		if err := rows.Scan(&schema, &rel, &name, &oid, &typmod, &notNull, &typname, &typtype, &elemOID, &typeSchema, &elemName, &elemType, &elemSchema, &comment); err != nil {
			return err
		}
		key := schema + "." + rel
		i, ok := idx[key]
		if !ok {
			continue
		}
		col := Column{
			Name:       name,
			TypeOID:    oid,
			TypeName:   typname,
			TypeMod:    typmod,
			NotNull:    notNull,
			TypeType:   firstByte(typtype),
			ElemOID:    elemOID,
			ElemName:   elemName,
			ElemType:   firstByte(elemType),
			Comment:    comment,
			TypeSchema: typeSchema,
			ElemSchema: elemSchema,
		}
		snap.Relations[i].Columns = append(snap.Relations[i].Columns, col)
	}
	return rows.Err()
}

func loadEnums(ctx context.Context, conn *pgx.Conn, schemas []string, snap *Snapshot) error {
	const q = `
SELECT n.nspname, t.typname, t.oid, COALESCE(obj_description(t.oid, 'pg_type'), ''), e.enumlabel
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
JOIN pg_enum e ON e.enumtypid = t.oid
WHERE n.nspname = ANY($1)
  AND t.typtype = 'e'
ORDER BY n.nspname, t.typname, e.enumsortorder`
	rows, err := conn.Query(ctx, q, schemas)
	if err != nil {
		return fmt.Errorf("list enums: %w", err)
	}
	defer rows.Close()
	byKey := map[string]int{}
	for rows.Next() {
		var schema, name, comment, label string
		var oid uint32
		if err := rows.Scan(&schema, &name, &oid, &comment, &label); err != nil {
			return err
		}
		key := schema + "." + name
		i, ok := byKey[key]
		if !ok {
			snap.Enums = append(snap.Enums, Enum{Schema: schema, Name: name, OID: oid, Comment: comment})
			i = len(snap.Enums) - 1
			byKey[key] = i
		}
		snap.Enums[i].Labels = append(snap.Enums[i].Labels, label)
	}
	return rows.Err()
}

func firstByte(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}
