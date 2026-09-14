package catalog

// Snapshot is the extracted PostgreSQL schema (no live connection required).
type Snapshot struct {
	Relations []Relation
	Enums     []Enum
}

type Relation struct {
	Schema  string
	Name    string
	Kind    string // r, v, m
	Comment string
	Columns []Column
}

func (r Relation) Key() string { return r.Schema + "." + r.Name }

type Column struct {
	Name       string
	TypeOID    uint32
	TypeName   string
	TypeMod    int32
	NotNull    bool
	TypeType   byte // pg_type.typtype
	ElemOID    uint32
	ElemName   string
	ElemType   byte
	Comment    string
	TypeSchema string
	ElemSchema string
}

type Enum struct {
	Schema  string
	Name    string
	OID     uint32
	Comment string
	Labels  []string
}

func (e Enum) Key() string { return e.Schema + "." + e.Name }
