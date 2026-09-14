package mapping

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/catalog"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/naming"
)

const datePattern = `^\d{4}-\d{2}-\d{2}$`

type Schema struct {
	Relations []Relation
	Enums     []Enum
}

type Relation struct {
	Schema    string
	Name      string
	ProtoName string
	Comment   string
	Fields    []Field
}

func (r Relation) Key() string { return r.Schema + "." + r.Name }

type Field struct {
	Column    string
	ProtoName string
	ProtoType string
	PGType    string
	Repeated  bool
	Optional  bool
	Comment   string
	Imports   []string
	Validate  []string
	Custom    bool // override replaced the proto type
	Extra     bool // yaml extra field, not a catalog column
}

type Enum struct {
	Schema    string
	Name      string
	ProtoName string
	Comment   string
	OID       uint32
	Values    []EnumValue
}

func (e Enum) Key() string { return e.Schema + "." + e.Name }

type EnumValue struct {
	Label     string
	ProtoName string
}

type TypeResolver interface {
	TypeForOID(oid uint32) (*pgtype.Type, bool)
	RegisterType(t *pgtype.Type)
}

type TypeLoader interface {
	LoadType(typeName string) (*pgtype.Type, error)
}

func Apply(snap *catalog.Snapshot, cfg *config.Config, resolver TypeResolver, loader TypeLoader) (*Schema, error) {
	if resolver == nil {
		resolver = pgtype.NewMap()
	}
	out := &Schema{}
	enumByOID := map[uint32]catalog.Enum{}
	enumByName := map[string]catalog.Enum{}
	for _, e := range snap.Enums {
		enumByOID[e.OID] = e
		enumByName[e.Key()] = e
		me := Enum{
			Schema:    e.Schema,
			Name:      e.Name,
			ProtoName: naming.PascalCase(e.Name),
			Comment:   e.Comment,
			OID:       e.OID,
		}
		for _, lab := range e.Labels {
			me.Values = append(me.Values, EnumValue{
				Label:     lab,
				ProtoName: naming.EnumValueName(me.ProtoName, lab),
			})
		}
		out.Enums = append(out.Enums, me)
	}

	for _, rel := range snap.Relations {
		mr := Relation{
			Schema:    rel.Schema,
			Name:      rel.Name,
			ProtoName: naming.PascalCase(rel.Name),
			Comment:   rel.Comment,
		}
		for _, col := range rel.Columns {
			if cfg.OmitsColumn(rel.Schema, rel.Name, col.Name) {
				continue
			}
			f, err := mapColumn(col, rel, cfg, resolver, loader, enumByOID, enumByName)
			if err != nil {
				return nil, fmt.Errorf("%s.%s.%s: %w", rel.Schema, rel.Name, col.Name, err)
			}
			mr.Fields = append(mr.Fields, f)
		}
		extras, err := extraFields(rel, cfg, mr.Fields)
		if err != nil {
			return nil, err
		}
		mr.Fields = append(mr.Fields, extras...)
		out.Relations = append(out.Relations, mr)
	}
	return out, nil
}

func mapColumn(
	col catalog.Column,
	rel catalog.Relation,
	cfg *config.Config,
	resolver TypeResolver,
	loader TypeLoader,
	enumByOID map[uint32]catalog.Enum,
	enumByName map[string]catalog.Enum,
) (Field, error) {
	f := Field{
		Column:    col.Name,
		ProtoName: naming.FieldName(col.Name),
		PGType:    col.TypeName,
		Comment:   col.Comment,
		Optional:  !col.NotNull,
	}

	baseOID := col.TypeOID
	baseName := col.TypeName
	baseSchema := col.TypeSchema
	if col.TypeType == 'a' || col.ElemOID != 0 {
		if col.ElemType == 'a' {
			return f, fmt.Errorf("multi-dimensional arrays are not supported")
		}
		f.Repeated = true
		f.Optional = false
		baseOID = col.ElemOID
		baseName = col.ElemName
		baseSchema = col.ElemSchema
		if baseOID == 0 {
			return f, fmt.Errorf("array element type is unknown")
		}
	}

	if e, ok := enumByOID[baseOID]; ok {
		f.PGType = e.Name
		f.ProtoType = naming.PascalCase(e.Name)
		f.Imports = append(f.Imports, e.Schema+"/"+e.Name+".proto")
	} else {
		mapped, err := mapOID(baseOID, baseName, baseSchema, col.TypeMod, cfg, resolver, loader, enumByName)
		if err != nil {
			return f, err
		}
		f.ProtoType = mapped.ProtoType
		f.PGType = mapped.PGType
		f.Imports = append(f.Imports, mapped.Imports...)
		f.Validate = append(f.Validate, mapped.Validate...)
	}

	applyOverrides(&f, rel, col, cfg, baseName)
	if isMessageType(f.ProtoType) && !f.Repeated {
		f.Optional = !col.NotNull
	}

	if !cfg.Options.Validate {
		f.Validate = nil
	} else {
		rewriteRepeatedValidate(&f)
	}
	f.Imports = uniq(f.Imports)
	return f, nil
}

func extraFields(rel catalog.Relation, cfg *config.Config, existing []Field) ([]Field, error) {
	var specs []config.ExtraField
	specs = append(specs, cfg.Fields.Extra...)
	if msg, ok := cfg.Messages[rel.Schema+"."+rel.Name]; ok {
		specs = append(specs, msg.Extra...)
	}
	if len(specs) == 0 {
		return nil, nil
	}

	taken := map[string]struct{}{}
	for _, f := range existing {
		taken[f.Column] = struct{}{}
		taken[f.ProtoName] = struct{}{}
	}

	var out []Field
	for _, spec := range specs {
		f, err := mapExtra(rel, cfg, spec, taken)
		if err != nil {
			return nil, err
		}
		taken[f.Column] = struct{}{}
		taken[f.ProtoName] = struct{}{}
		out = append(out, f)
	}
	return out, nil
}

func mapExtra(rel catalog.Relation, cfg *config.Config, spec config.ExtraField, taken map[string]struct{}) (Field, error) {
	if spec.Name == "" {
		return Field{}, fmt.Errorf("%s: extra field is missing name", rel.Key())
	}
	if spec.ProtoType == "" {
		return Field{}, fmt.Errorf("%s.%s: extra field is missing proto_type", rel.Key(), spec.Name)
	}
	if spec.Optional && spec.Repeated {
		return Field{}, fmt.Errorf("%s.%s: extra field cannot be both optional and repeated", rel.Key(), spec.Name)
	}
	protoName := naming.FieldName(spec.Name)
	if _, ok := taken[spec.Name]; ok {
		return Field{}, fmt.Errorf("%s.%s: extra field collides with an existing field", rel.Key(), spec.Name)
	}
	if protoName != spec.Name {
		if _, ok := taken[protoName]; ok {
			return Field{}, fmt.Errorf("%s.%s: extra field proto name %s collides with an existing field", rel.Key(), spec.Name, protoName)
		}
	}
	if !isScalarProto(spec.ProtoType) && spec.Import == "" && wellKnownImport(spec.ProtoType) == "" {
		return Field{}, fmt.Errorf("%s.%s: extra field type %s requires import", rel.Key(), spec.Name, spec.ProtoType)
	}

	f := Field{
		Column:    spec.Name,
		ProtoName: protoName,
		ProtoType: spec.ProtoType,
		PGType:    "extra",
		Repeated:  spec.Repeated,
		Optional:  spec.Optional,
		Comment:   spec.Comment,
		Custom:    true,
		Extra:     true,
	}
	if spec.Import != "" {
		f.Imports = append(f.Imports, spec.Import)
	} else if wkt := wellKnownImport(spec.ProtoType); wkt != "" {
		f.Imports = append(f.Imports, wkt)
	}
	if cfg.Options.Validate {
		f.Validate = append(f.Validate, validateOpts(spec.Validate)...)
		rewriteRepeatedValidate(&f)
	}
	f.Imports = uniq(f.Imports)
	return f, nil
}

func isScalarProto(t string) bool {
	switch t {
	case "bool", "int32", "int64", "uint32", "uint64", "sint32", "sint64",
		"fixed32", "fixed64", "sfixed32", "sfixed64", "float", "double", "string", "bytes":
		return true
	default:
		return false
	}
}

func wellKnownImport(protoType string) string {
	switch protoType {
	case "google.protobuf.Timestamp":
		return "google/protobuf/timestamp.proto"
	case "google.protobuf.Duration":
		return "google/protobuf/duration.proto"
	case "google.protobuf.Empty":
		return "google/protobuf/empty.proto"
	case "google.protobuf.Any":
		return "google/protobuf/any.proto"
	case "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue":
		return "google/protobuf/struct.proto"
	case "google.protobuf.BoolValue", "google.protobuf.BytesValue", "google.protobuf.DoubleValue",
		"google.protobuf.FloatValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value",
		"google.protobuf.StringValue", "google.protobuf.UInt32Value", "google.protobuf.UInt64Value":
		return "google/protobuf/wrappers.proto"
	case "google.protobuf.FieldMask":
		return "google/protobuf/field_mask.proto"
	default:
		return ""
	}
}

type mappedType struct {
	ProtoType string
	PGType    string
	Imports   []string
	Validate  []string
}

func mapOID(oid uint32, typeName, typeSchema string, typmod int32, cfg *config.Config, resolver TypeResolver, loader TypeLoader, enumByName map[string]catalog.Enum) (mappedType, error) {
	t, ok := resolver.TypeForOID(oid)
	if !ok && loader != nil && typeName != "" {
		qual := typeName
		if typeSchema != "" {
			qual = typeSchema + "." + typeName
		}
		loaded, err := loader.LoadType(qual)
		if err == nil && loaded != nil {
			resolver.RegisterType(loaded)
			t, ok = loaded, true
		}
	}

	if ok {
		if ac, okArr := t.Codec.(*pgtype.ArrayCodec); okArr && ac.ElementType != nil {
			return mapOID(ac.ElementType.OID, ac.ElementType.Name, typeSchema, typmod, cfg, resolver, loader, enumByName)
		}
		if mt, err := fromOID(t.OID, t.Name, typmod, cfg); err == nil {
			return mt, nil
		}
		if mt, okCodec := fromCodec(t); okCodec {
			return mt, nil
		}
		if _, isComposite := t.Codec.(*pgtype.CompositeCodec); isComposite || t.OID == pgtype.RecordOID {
			if cfg.Options.StrictTypes {
				return mappedType{}, fmt.Errorf("unsupported PostgreSQL type %s (oid %d); add an override or omit --strict-types", typeName, oid)
			}
			return anyFallback(t.Name), nil
		}
	}

	if mt, err := fromOID(oid, typeName, typmod, cfg); err == nil {
		return mt, nil
	}

	key := typeSchema + "." + typeName
	if e, ok := enumByName[key]; ok {
		return mappedType{ProtoType: naming.PascalCase(e.Name), PGType: e.Name, Imports: []string{e.Schema + "/" + e.Name + ".proto"}}, nil
	}

	if ov, ok := cfg.Overrides.Types[typeName]; ok && ov.ProtoType != "" {
		mt := mappedType{ProtoType: ov.ProtoType, PGType: typeName}
		if ov.Import != "" {
			mt.Imports = []string{ov.Import}
		}
		return mt, nil
	}

	if cfg.Options.StrictTypes {
		return mappedType{}, fmt.Errorf("unsupported PostgreSQL type %s (oid %d); add an override or omit --strict-types", typeName, oid)
	}
	return anyFallback(typeName), nil
}

func anyFallback(typeName string) mappedType {
	return mappedType{
		ProtoType: "google.protobuf.Any",
		PGType:    typeName,
		Imports:   []string{"google/protobuf/any.proto"},
	}
}

func fromOID(oid uint32, typeName string, typmod int32, cfg *config.Config) (mappedType, error) {
	mt := mappedType{PGType: typeName}
	switch oid {
	case pgtype.BoolOID:
		mt.ProtoType = "bool"
	case pgtype.Int2OID, pgtype.Int4OID:
		mt.ProtoType = "int32"
	case pgtype.Int8OID:
		mt.ProtoType = "int64"
	case pgtype.Float4OID:
		mt.ProtoType = "float"
	case pgtype.Float8OID:
		mt.ProtoType = "double"
	case pgtype.TextOID, pgtype.BPCharOID, pgtype.NameOID, pgtype.QCharOID:
		mt.ProtoType = "string"
	case pgtype.VarcharOID:
		mt.ProtoType = "string"
		if n := varcharLen(typmod); n > 0 && cfg.Options.Validate {
			mt.Validate = append(mt.Validate, "(buf.validate.field).string.max_len = "+strconv.Itoa(n))
		}
	case pgtype.ByteaOID:
		mt.ProtoType = "bytes"
	case pgtype.UUIDOID:
		mt.ProtoType = "string"
		if cfg.Options.Validate {
			mt.Validate = append(mt.Validate, "(buf.validate.field).string.uuid = true")
		}
	case pgtype.JSONOID, pgtype.JSONBOID:
		if cfg.Options.JSONBAsStruct {
			mt.ProtoType = "google.protobuf.Struct"
			mt.Imports = []string{"google/protobuf/struct.proto"}
		} else {
			mt.ProtoType = "string"
		}
	case pgtype.TimestamptzOID:
		mt.ProtoType = "google.protobuf.Timestamp"
		mt.Imports = []string{"google/protobuf/timestamp.proto"}
	case pgtype.TimestampOID, pgtype.TimeOID, pgtype.TimetzOID, pgtype.IntervalOID:
		mt.ProtoType = "string"
	case pgtype.DateOID:
		mt.ProtoType = "string"
		if cfg.Options.Validate {
			mt.Validate = append(mt.Validate, "(buf.validate.field).string.pattern = "+strconv.Quote(datePattern))
		}
	case pgtype.NumericOID:
		mt.ProtoType = "string"
	case pgtype.InetOID, pgtype.CIDROID, pgtype.MacaddrOID, pgtype.Macaddr8OID:
		mt.ProtoType = "string"
	case pgtype.BitOID, pgtype.VarbitOID, pgtype.XMLOID, pgtype.JSONPathOID:
		mt.ProtoType = "string"
	case pgtype.PointOID, pgtype.LsegOID, pgtype.PathOID, pgtype.BoxOID, pgtype.PolygonOID, pgtype.LineOID, pgtype.CircleOID:
		mt.ProtoType = "string"
	case pgtype.Int4rangeOID, pgtype.Int8rangeOID, pgtype.NumrangeOID, pgtype.TsrangeOID, pgtype.TstzrangeOID, pgtype.DaterangeOID,
		pgtype.Int4multirangeOID, pgtype.Int8multirangeOID, pgtype.NummultirangeOID, pgtype.TsmultirangeOID, pgtype.TstzmultirangeOID, pgtype.DatemultirangeOID:
		mt.ProtoType = "string"
	case pgtype.OIDOID, pgtype.XIDOID, pgtype.CIDOID, pgtype.TIDOID, pgtype.XID8OID:
		mt.ProtoType = "uint64"
		if oid == pgtype.OIDOID || oid == pgtype.CIDOID {
			mt.ProtoType = "uint32"
		}
	default:
		return mt, fmt.Errorf("no mapping")
	}
	return mt, nil
}

func fromCodec(t *pgtype.Type) (mappedType, bool) {
	if t == nil || t.Codec == nil {
		return mappedType{}, false
	}
	switch t.Codec.(type) {
	case pgtype.BoolCodec:
		return mappedType{ProtoType: "bool", PGType: t.Name}, true
	case pgtype.Int2Codec, pgtype.Int4Codec:
		return mappedType{ProtoType: "int32", PGType: t.Name}, true
	case pgtype.Int8Codec:
		return mappedType{ProtoType: "int64", PGType: t.Name}, true
	case pgtype.Float4Codec:
		return mappedType{ProtoType: "float", PGType: t.Name}, true
	case pgtype.Float8Codec:
		return mappedType{ProtoType: "double", PGType: t.Name}, true
	case pgtype.TextCodec, pgtype.QCharCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.ByteaCodec:
		return mappedType{ProtoType: "bytes", PGType: t.Name}, true
	case pgtype.UUIDCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case *pgtype.JSONCodec, *pgtype.JSONBCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case *pgtype.TimestamptzCodec:
		return mappedType{ProtoType: "google.protobuf.Timestamp", PGType: t.Name, Imports: []string{"google/protobuf/timestamp.proto"}}, true
	case *pgtype.TimestampCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.DateCodec, pgtype.TimeCodec, pgtype.IntervalCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.NumericCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.InetCodec, pgtype.MacaddrCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.BitsCodec, *pgtype.XMLCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case pgtype.PointCodec, pgtype.LsegCodec, pgtype.PathCodec, pgtype.BoxCodec, pgtype.PolygonCodec, pgtype.LineCodec, pgtype.CircleCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	case *pgtype.RangeCodec, *pgtype.MultirangeCodec:
		return mappedType{ProtoType: "string", PGType: t.Name}, true
	default:
		return mappedType{}, false
	}
}

func applyOverrides(f *Field, rel catalog.Relation, col catalog.Column, cfg *config.Config, baseName string) {
	ov, ok := cfg.Overrides.Types[baseName]
	if !ok {
		ov, ok = cfg.Overrides.Types[col.TypeName]
	}
	colKey := rel.Schema + "." + rel.Name + "." + col.Name
	if cov, cok := cfg.Overrides.Columns[colKey]; cok {
		ov = cov
		ok = true
	}
	if !ok {
		return
	}
	if ov.ProtoType != "" {
		f.ProtoType = ov.ProtoType
		f.Custom = true
	}
	if ov.Import != "" {
		f.Imports = append(f.Imports, ov.Import)
	}
	if cfg.Options.Validate {
		f.Validate = append(f.Validate, validateOpts(ov.Validate)...)
	}
}

func validateOpts(v config.Validate) []string {
	var out []string
	if v.Email != nil && *v.Email {
		out = append(out, "(buf.validate.field).string.email = true")
	}
	if v.UUID != nil && *v.UUID {
		out = append(out, "(buf.validate.field).string.uuid = true")
	}
	if v.MaxLen != nil {
		out = append(out, "(buf.validate.field).string.max_len = "+strconv.Itoa(*v.MaxLen))
	}
	if v.Pattern != "" {
		out = append(out, "(buf.validate.field).string.pattern = "+strconv.Quote(v.Pattern))
	}
	if v.CEL != "" {
		out = append(out, `(buf.validate.field).cel = {expression: `+strconv.Quote(v.CEL)+`}`)
	}
	return out
}

func rewriteRepeatedValidate(f *Field) {
	if !f.Repeated {
		return
	}
	for i, v := range f.Validate {
		v = strings.Replace(v, "(buf.validate.field).string.", "(buf.validate.field).repeated.items.string.", 1)
		f.Validate[i] = v
	}
}

func varcharLen(typmod int32) int {
	if typmod < 4 {
		return 0
	}
	return int(typmod - 4)
}

func isMessageType(t string) bool {
	return t != "" && !isScalarProto(t)
}

func uniq(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
