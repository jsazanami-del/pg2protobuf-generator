package lock

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/mapping"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/naming"
)

const Version = "1"

type File struct {
	Version  string              `yaml:"version"`
	Messages map[string]*Message `yaml:"messages"`
	Enums    map[string]*Enum    `yaml:"enums"`
}

type Message struct {
	ProtoName       string            `yaml:"proto_name"`
	Fields          map[string]*Field `yaml:"fields"`
	ReservedNumbers []int             `yaml:"reserved_numbers"`
	ReservedNames   []string          `yaml:"reserved_names"`
	Renames         map[string]string `yaml:"renames,omitempty"`
}

type Field struct {
	Number    int    `yaml:"number"`
	ProtoType string `yaml:"proto_type"`
	PGType    string `yaml:"pg_type"`
	ProtoName string `yaml:"proto_name,omitempty"`
}

type Enum struct {
	ProtoName       string            `yaml:"proto_name"`
	Values          map[string]*Value `yaml:"values"`
	ReservedNumbers []int             `yaml:"reserved_numbers"`
	ReservedNames   []string          `yaml:"reserved_names"`
	Renames         map[string]string `yaml:"renames,omitempty"`
}

type Value struct {
	Number    int    `yaml:"number"`
	ProtoName string `yaml:"proto_name"`
}

type Result struct {
	Lock     *File
	IR       *IR
	Breaking []string
}

// IR is the numbered representation passed to templates.
type IR struct {
	Files []*ProtoFile
}

type ProtoFile struct {
	Path      string
	Schema    string
	Package   string
	GoPackage string
	Imports   []string
	Messages  []*IRMessage
	Enums     []*IREnum
}

type IRMessage struct {
	Name            string
	Comment         string
	Fields          []*IRField
	ReservedNumbers []int
	ReservedNames   []string
}

type IRField struct {
	Label   string
	Type    string
	Name    string
	Number  int
	Comment string
	Options []string
}

type IREnum struct {
	Name            string
	Comment         string
	Values          []*IREnumValue
	ReservedNumbers []int
	ReservedNames   []string
}

type IREnumValue struct {
	Name    string
	Number  int
	Comment string
}

func Empty() *File {
	return &File{
		Version:  Version,
		Messages: map[string]*Message{},
		Enums:    map[string]*Enum{},
	}
}

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Empty(), nil
		}
		return nil, err
	}
	f := Empty()
	if err := yaml.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Messages == nil {
		f.Messages = map[string]*Message{}
	}
	if f.Enums == nil {
		f.Enums = map[string]*Enum{}
	}
	return f, nil
}

func Assign(old *File, mapped *mapping.Schema, cfg *config.Config) *Result {
	if old == nil {
		old = Empty()
	}
	next := Empty()
	var breaking []string

	currentRel := map[string]mapping.Relation{}
	for _, r := range mapped.Relations {
		currentRel[r.Key()] = r
	}
	currentEnum := map[string]mapping.Enum{}
	for _, e := range mapped.Enums {
		currentEnum[e.Key()] = e
	}

	for key, rel := range currentRel {
		oldMsg := old.Messages[key]
		if oldMsg != nil {
			oldMsg = cloneMessage(oldMsg)
			applyFieldRenames(oldMsg, cfg.Renames.Columns, key)
		}
		msg, reasons := assignMessage(oldMsg, rel)
		next.Messages[key] = msg
		breaking = append(breaking, reasons...)
	}

	for key := range old.Messages {
		if _, ok := currentRel[key]; !ok {
			next.Messages[key] = cloneMessage(old.Messages[key])
			breaking = append(breaking, fmt.Sprintf("relation %s is missing from the database (use --prune to drop generated files)", key))
		}
	}

	for key, en := range currentEnum {
		oldEn := old.Enums[key]
		if oldEn != nil {
			oldEn = cloneEnum(oldEn)
			applyEnumRenames(oldEn, cfg.Renames.EnumValues, key)
		}
		ne, reasons := assignEnum(oldEn, en)
		next.Enums[key] = ne
		breaking = append(breaking, reasons...)
	}
	for key := range old.Enums {
		if _, ok := currentEnum[key]; !ok {
			if excludedEnum(key, cfg) {
				breaking = append(breaking, fmt.Sprintf("enum %s is excluded (use --force to drop it from the lock; --prune to delete generated files)", key))
				continue
			}
			next.Enums[key] = cloneEnum(old.Enums[key])
			breaking = append(breaking, fmt.Sprintf("enum %s is missing from the database (use --prune to drop generated files)", key))
		}
	}

	ir := buildIR(mapped, next, cfg)
	return &Result{Lock: next, IR: ir, Breaking: breaking}
}

func applyFieldRenames(msg *Message, renames map[string]string, relKey string) {
	if msg.Renames != nil {
		for oldN, newN := range msg.Renames {
			moveField(msg, oldN, newN)
		}
	}
	prefix := relKey + "."
	for from, to := range renames {
		if strings.HasPrefix(from, prefix) {
			oldCol := strings.TrimPrefix(from, prefix)
			moveField(msg, oldCol, to)
			continue
		}
		// allow schema.rel.col keys only
		if from == relKey+"."+to {
			continue
		}
	}
}

func moveField(msg *Message, oldN, newN string) {
	if oldN == newN {
		return
	}
	f, ok := msg.Fields[oldN]
	if !ok {
		return
	}
	delete(msg.Fields, oldN)
	msg.Fields[newN] = f
}

func applyEnumRenames(en *Enum, renames map[string]string, enumKey string) {
	if en.Renames != nil {
		for oldN, newN := range en.Renames {
			moveValue(en, oldN, newN)
		}
	}
	prefix := enumKey + "."
	for from, to := range renames {
		if strings.HasPrefix(from, prefix) {
			moveValue(en, strings.TrimPrefix(from, prefix), to)
		}
	}
}

func moveValue(en *Enum, oldN, newN string) {
	if oldN == newN {
		return
	}
	v, ok := en.Values[oldN]
	if !ok {
		return
	}
	delete(en.Values, oldN)
	en.Values[newN] = v
}

func assignMessage(old *Message, rel mapping.Relation) (*Message, []string) {
	var reasons []string
	msg := &Message{
		ProtoName:       rel.ProtoName,
		Fields:          map[string]*Field{},
		ReservedNumbers: nil,
		ReservedNames:   nil,
		Renames:         map[string]string{},
	}
	used := map[int]bool{}
	if old != nil {
		msg.ReservedNumbers = append([]int{}, old.ReservedNumbers...)
		msg.ReservedNames = append([]string{}, old.ReservedNames...)
		msg.Renames = cloneMap(old.Renames)
		for _, n := range msg.ReservedNumbers {
			used[n] = true
		}
	}

	currentCols := map[string]mapping.Field{}
	for _, f := range rel.Fields {
		currentCols[f.Column] = f
	}

	catalogDropped := 0
	catalogAdded := 0
	if old != nil {
		for col, of := range old.Fields {
			used[of.Number] = true
			if _, ok := currentCols[col]; !ok {
				msg.ReservedNumbers = append(msg.ReservedNumbers, of.Number)
				msg.ReservedNames = append(msg.ReservedNames, col)
				used[of.Number] = true
				if of.PGType != "extra" {
					catalogDropped++
				}
			}
		}
	}

	for _, f := range rel.Fields {
		entry := &Field{ProtoType: protoTypeKey(f), PGType: f.PGType, ProtoName: f.ProtoName}
		if old != nil {
			if of, ok := old.Fields[f.Column]; ok {
				entry.Number = of.Number
				if of.ProtoType != entry.ProtoType {
					reasons = append(reasons, fmt.Sprintf("%s.%s: proto type changed %s -> %s", rel.Key(), f.Column, of.ProtoType, entry.ProtoType))
				}
				msg.Fields[f.Column] = entry
				continue
			}
		}
		if !f.Extra {
			catalogAdded++
		}
		n := nextNumber(used)
		used[n] = true
		entry.Number = n
		msg.Fields[f.Column] = entry
	}

	if catalogDropped > 0 && catalogAdded > 0 {
		reasons = append(reasons, fmt.Sprintf("%s: columns removed and added without a rename declaration", rel.Key()))
	}
	msg.ReservedNumbers = uniqInts(msg.ReservedNumbers)
	msg.ReservedNames = uniqStrings(msg.ReservedNames)
	return msg, reasons
}

func assignEnum(old *Enum, en mapping.Enum) (*Enum, []string) {
	var reasons []string
	out := &Enum{
		ProtoName:       en.ProtoName,
		Values:          map[string]*Value{},
		ReservedNumbers: nil,
		ReservedNames:   nil,
		Renames:         map[string]string{},
	}
	used := map[int]bool{0: true}
	if old != nil {
		out.ReservedNumbers = append([]int{}, old.ReservedNumbers...)
		out.ReservedNames = append([]string{}, old.ReservedNames...)
		out.Renames = cloneMap(old.Renames)
		for _, n := range out.ReservedNumbers {
			used[n] = true
		}
	}
	current := map[string]mapping.EnumValue{}
	for _, v := range en.Values {
		current[v.Label] = v
	}
	dropped, added := 0, 0
	if old != nil {
		for lab, ov := range old.Values {
			used[ov.Number] = true
			if _, ok := current[lab]; !ok {
				dropped++
				out.ReservedNumbers = append(out.ReservedNumbers, ov.Number)
				out.ReservedNames = append(out.ReservedNames, lab)
			}
		}
	}
	for _, v := range en.Values {
		entry := &Value{ProtoName: v.ProtoName}
		if old != nil {
			if ov, ok := old.Values[v.Label]; ok {
				entry.Number = ov.Number
				entry.ProtoName = ov.ProtoName
				if ov.ProtoName == "" {
					entry.ProtoName = v.ProtoName
				}
				out.Values[v.Label] = entry
				continue
			}
		}
		added++
		n := nextNumber(used)
		used[n] = true
		entry.Number = n
		out.Values[v.Label] = entry
	}
	if dropped > 0 && added > 0 {
		reasons = append(reasons, fmt.Sprintf("%s: enum labels removed and added without a rename declaration", en.Key()))
	}
	out.ReservedNumbers = uniqInts(out.ReservedNumbers)
	out.ReservedNames = uniqStrings(out.ReservedNames)
	return out, reasons
}

func protoTypeKey(f mapping.Field) string {
	if f.Repeated {
		return "repeated " + f.ProtoType
	}
	return f.ProtoType
}

func nextNumber(used map[int]bool) int {
	n := 1
	for {
		if n >= 19000 && n <= 19999 {
			n = 20000
		}
		if !used[n] {
			return n
		}
		n++
	}
}

func buildIR(mapped *mapping.Schema, lk *File, cfg *config.Config) *IR {
	filesByPath := map[string]*ProtoFile{}
	file := func(schema, name string) *ProtoFile {
		path := schema + "/" + name + ".proto"
		if f, ok := filesByPath[path]; ok {
			return f
		}
		f := &ProtoFile{
			Path:      path,
			Schema:    schema,
			Package:   joinDots(cfg.Proto.PackagePrefix, schema),
			GoPackage: joinSlash(cfg.Proto.GoPackagePrefix, schema),
		}
		filesByPath[path] = f
		return f
	}

	for _, en := range mapped.Enums {
		le := lk.Enums[en.Key()]
		f := file(en.Schema, en.Name)
		ie := &IREnum{Name: en.ProtoName, Comment: en.Comment}
		if le != nil {
			ie.ReservedNumbers = le.ReservedNumbers
			ie.ReservedNames = protoReservedNames(le.ReservedNames, en.ProtoName)
		}
		ie.Values = append(ie.Values, &IREnumValue{
			Name:   naming.UnspecifiedName(en.ProtoName),
			Number: 0,
		})
		for _, v := range en.Values {
			num := 0
			pname := v.ProtoName
			if le != nil {
				if lv, ok := le.Values[v.Label]; ok {
					num = lv.Number
					if lv.ProtoName != "" {
						pname = lv.ProtoName
					}
				}
			}
			ie.Values = append(ie.Values, &IREnumValue{Name: pname, Number: num})
		}
		sort.SliceStable(ie.Values[1:], func(i, j int) bool {
			return ie.Values[i+1].Number < ie.Values[j+1].Number
		})
		f.Enums = append(f.Enums, ie)
	}

	for _, rel := range mapped.Relations {
		lm := lk.Messages[rel.Key()]
		f := file(rel.Schema, rel.Name)
		im := &IRMessage{Name: rel.ProtoName, Comment: rel.Comment}
		if lm != nil {
			im.ReservedNumbers = lm.ReservedNumbers
			im.ReservedNames = lm.ReservedNames
		}
		var imports []string
		for _, field := range rel.Fields {
			num := 0
			if lm != nil {
				if lf, ok := lm.Fields[field.Column]; ok {
					num = lf.Number
				}
			}
			label := ""
			if field.Repeated {
				label = "repeated"
			} else if field.Optional {
				label = "optional"
			}
			im.Fields = append(im.Fields, &IRField{
				Label:   label,
				Type:    field.ProtoType,
				Name:    field.ProtoName,
				Number:  num,
				Comment: field.Comment,
				Options: field.Validate,
			})
			imports = append(imports, field.Imports...)
		}
		if lm != nil {
			sort.SliceStable(im.Fields, func(i, j int) bool {
				return im.Fields[i].Number < im.Fields[j].Number
			})
		}
		f.Messages = append(f.Messages, im)
		f.Imports = append(f.Imports, imports...)
		if hasValidate(im) && cfg.Options.Validate {
			f.Imports = append(f.Imports, "buf/validate/validate.proto")
		}
		f.Imports = uniqSorted(f.Imports)
	}

	ir := &IR{}
	var paths []string
	for p := range filesByPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		ir.Files = append(ir.Files, filesByPath[p])
	}
	return ir
}

func excludedEnum(key string, cfg *config.Config) bool {
	schema, name, ok := strings.Cut(key, ".")
	if !ok {
		return cfg.ExcludesObject("", key)
	}
	return cfg.ExcludesObject(schema, name)
}

func protoReservedNames(labels []string, enumPascal string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, naming.EnumValueName(enumPascal, l))
	}
	return out
}

func hasValidate(m *IRMessage) bool {
	for _, f := range m.Fields {
		if len(f.Options) > 0 {
			return true
		}
	}
	return false
}

func joinDots(prefix, schema string) string {
	prefix = strings.Trim(prefix, ".")
	if prefix == "" {
		return schema
	}
	if schema == "" {
		return prefix
	}
	return prefix + "." + schema
}

func joinSlash(prefix, schema string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return schema
	}
	if schema == "" {
		return prefix
	}
	return prefix + "/" + schema
}

func cloneMessage(m *Message) *Message {
	c := *m
	c.Fields = map[string]*Field{}
	for k, v := range m.Fields {
		fv := *v
		c.Fields[k] = &fv
	}
	c.ReservedNumbers = append([]int{}, m.ReservedNumbers...)
	c.ReservedNames = append([]string{}, m.ReservedNames...)
	c.Renames = cloneMap(m.Renames)
	return &c
}

func cloneEnum(e *Enum) *Enum {
	c := *e
	c.Values = map[string]*Value{}
	for k, v := range e.Values {
		vv := *v
		c.Values[k] = &vv
	}
	c.ReservedNumbers = append([]int{}, e.ReservedNumbers...)
	c.ReservedNames = append([]string{}, e.ReservedNames...)
	c.Renames = cloneMap(e.Renames)
	return &c
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func uniqInts(in []int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, n := range in {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

func uniqStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func uniqSorted(in []string) []string {
	return uniqStrings(in)
}

func Write(path string, f *File) error {
	data, err := Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Marshal(f *File) ([]byte, error) {
	n := &yaml.Node{Kind: yaml.MappingNode}
	addKV := func(k string, v *yaml.Node) {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, v)
	}
	addKV("version", scalar(f.Version))
	addKV("messages", mapNode(f.Messages, marshalMessage))
	addKV("enums", mapNode(f.Enums, marshalEnum))
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{n}}
	return yaml.Marshal(doc)
}

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

func mapNode[T any](m map[string]T, enc func(T) *yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n.Content = append(n.Content, scalar(k), enc(m[k]))
	}
	return n
}

func marshalMessage(m *Message) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	put := func(k string, v *yaml.Node) {
		n.Content = append(n.Content, scalar(k), v)
	}
	put("proto_name", scalar(m.ProtoName))
	put("fields", mapNode(m.Fields, func(f *Field) *yaml.Node {
		fn := &yaml.Node{Kind: yaml.MappingNode}
		fn.Content = append(fn.Content,
			scalar("number"), intNode(f.Number),
			scalar("proto_type"), scalar(f.ProtoType),
			scalar("pg_type"), scalar(f.PGType),
		)
		if f.ProtoName != "" {
			fn.Content = append(fn.Content, scalar("proto_name"), scalar(f.ProtoName))
		}
		return fn
	}))
	put("reserved_numbers", intSeq(m.ReservedNumbers))
	put("reserved_names", strSeq(m.ReservedNames))
	if len(m.Renames) > 0 {
		put("renames", strMap(m.Renames))
	}
	return n
}

func marshalEnum(e *Enum) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	put := func(k string, v *yaml.Node) {
		n.Content = append(n.Content, scalar(k), v)
	}
	put("proto_name", scalar(e.ProtoName))
	put("values", mapNode(e.Values, func(v *Value) *yaml.Node {
		vn := &yaml.Node{Kind: yaml.MappingNode}
		vn.Content = append(vn.Content,
			scalar("number"), intNode(v.Number),
			scalar("proto_name"), scalar(v.ProtoName),
		)
		return vn
	}))
	put("reserved_numbers", intSeq(e.ReservedNumbers))
	put("reserved_names", strSeq(e.ReservedNames))
	if len(e.Renames) > 0 {
		put("renames", strMap(e.Renames))
	}
	return n
}

func intNode(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", n), Tag: "!!int"}
}

func intSeq(in []int) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode}
	for _, v := range in {
		n.Content = append(n.Content, intNode(v))
	}
	return n
}

func strSeq(in []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode}
	for _, v := range in {
		n.Content = append(n.Content, scalar(v))
	}
	return n
}

func strMap(m map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n.Content = append(n.Content, scalar(k), scalar(m[k]))
	}
	return n
}
