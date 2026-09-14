package naming

import (
	"strings"
	"unicode"
)

var protoKeywords = map[string]struct{}{
	"syntax": {}, "import": {}, "weak": {}, "public": {}, "option": {},
	"package": {}, "message": {}, "enum": {}, "service": {}, "rpc": {},
	"returns": {}, "stream": {}, "optional": {}, "repeated": {}, "required": {},
	"map": {}, "oneof": {}, "reserved": {}, "to": {}, "max": {},
	"true": {}, "false": {}, "inf": {}, "nan": {}, "group": {},
	"extensions": {}, "extend": {}, "default": {}, "packed": {},
	"float": {}, "double": {}, "int32": {}, "int64": {}, "uint32": {},
	"uint64": {}, "sint32": {}, "sint64": {}, "fixed32": {}, "fixed64": {},
	"sfixed32": {}, "sfixed64": {}, "bool": {}, "string": {}, "bytes": {},
}

// PascalCase converts snake_case / mixed identifiers to PascalCase.
// It does not singularize: users -> Users.
func PascalCase(name string) string {
	parts := splitIdent(name)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(capitalize(p))
	}
	s := b.String()
	if s == "" {
		return "X"
	}
	if !unicode.IsLetter(rune(s[0])) && s[0] != '_' {
		return "X" + s
	}
	return s
}

// FieldName keeps the PostgreSQL column name when it is a valid proto ident.
func FieldName(name string) string {
	s := sanitizeIdent(name)
	if _, ok := protoKeywords[s]; ok {
		return s + "_"
	}
	return s
}

// EnumValueName is ENUM_LABEL in UPPER_SNAKE, prefixed by the enum type name.
func EnumValueName(enumPascal, label string) string {
	prefix := toUpperSnake(enumPascal)
	lab := toUpperSnake(label)
	if lab == "" {
		lab = "VALUE"
	}
	if prefix == "" {
		return lab
	}
	return prefix + "_" + lab
}

func UnspecifiedName(enumPascal string) string {
	prefix := toUpperSnake(enumPascal)
	if prefix == "" {
		return "UNSPECIFIED"
	}
	return prefix + "_UNSPECIFIED"
}

func sanitizeIdent(name string) string {
	if name == "" {
		return "x_"
	}
	var b strings.Builder
	for i, r := range name {
		if i == 0 {
			if unicode.IsLetter(r) || r == '_' {
				b.WriteRune(r)
				continue
			}
			b.WriteByte('_')
			if unicode.IsDigit(r) {
				b.WriteRune(r)
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	s := b.String()
	if s == "" || s == "_" {
		return "x_"
	}
	return s
}

func splitIdent(name string) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	prevLower := false
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' || unicode.IsSpace(r) {
			flush()
			prevLower = false
			continue
		}
		if unicode.IsUpper(r) && prevLower {
			flush()
		}
		cur.WriteRune(unicode.ToLower(r))
		prevLower = unicode.IsLower(r)
	}
	flush()
	return parts
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	rs := []rune(s)
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}

func toUpperSnake(s string) string {
	parts := splitIdent(s)
	for i, p := range parts {
		parts[i] = strings.ToUpper(sanitizeIdent(p))
	}
	out := strings.Join(parts, "_")
	out = strings.Trim(out, "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	return out
}
