package config

import (
	"fmt"
	"os"
	pathpkg "path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// last proto package component must be a version (buf PACKAGE_VERSION_SUFFIX).
var packageVersionSuffix = regexp.MustCompile(`(?:^|\.)v(?:0|[1-9]\d*)(?:(?:alpha|beta|test|unstable)\d+)?$`)

const Version = "1"

// Config is .pg2proto.yaml.
type Config struct {
	Version   string                 `yaml:"version"`
	Proto     Proto                  `yaml:"proto"`
	Options   Options                `yaml:"options"`
	Tables    Tables                 `yaml:"tables"`
	Fields    Fields                 `yaml:"fields"`
	Messages  map[string]MessageSpec `yaml:"messages"`
	Renames   Renames                `yaml:"renames"`
	Overrides Overrides              `yaml:"overrides"`
}

type Proto struct {
	PackagePrefix   string            `yaml:"package_prefix"`
	GoPackagePrefix string            `yaml:"go_package_prefix"`
	Out             string            `yaml:"out"`
	Schemas         map[string]string `yaml:"schemas"`
}

type Options struct {
	JSONBAsStruct bool `yaml:"jsonb_as_struct"`
	Validate      bool `yaml:"validate"`
	StrictTypes   bool `yaml:"strict_types"`
}

type Tables struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type Fields struct {
	Omit  []string     `yaml:"omit"`
	Extra []ExtraField `yaml:"extra"`
}

type MessageSpec struct {
	Omit  []string     `yaml:"omit"`
	Extra []ExtraField `yaml:"extra"`
}

type ExtraField struct {
	Name      string   `yaml:"name"`
	ProtoType string   `yaml:"proto_type"`
	Optional  bool     `yaml:"optional"`
	Repeated  bool     `yaml:"repeated"`
	Import    string   `yaml:"import"`
	Validate  Validate `yaml:"validate"`
	Comment   string   `yaml:"comment"`
}

type Renames struct {
	Columns    map[string]string `yaml:"columns"`
	EnumValues map[string]string `yaml:"enum_values"`
}

type Overrides struct {
	Types   map[string]Override `yaml:"types"`
	Columns map[string]Override `yaml:"columns"`
}

type Override struct {
	ProtoType string   `yaml:"proto_type"`
	Import    string   `yaml:"import"`
	Validate  Validate `yaml:"validate"`
}

type Validate struct {
	Email   *bool  `yaml:"email"`
	UUID    *bool  `yaml:"uuid"`
	MaxLen  *int   `yaml:"max_len"`
	CEL     string `yaml:"cel"`
	Pattern string `yaml:"pattern"`
}

func Defaults() *Config {
	return &Config{
		Version: Version,
		Proto: Proto{
			PackagePrefix:   "db.v1",
			GoPackagePrefix: "",
			Schemas:         map[string]string{},
		},
		Options: Options{
			Validate: true,
		},
		Messages: map[string]MessageSpec{},
		Renames: Renames{
			Columns:    map[string]string{},
			EnumValues: map[string]string{},
		},
		Overrides: Overrides{
			Types:   map[string]Override{},
			Columns: map[string]Override{},
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Version == "" {
		cfg.Version = Version
	}
	if cfg.Renames.Columns == nil {
		cfg.Renames.Columns = map[string]string{}
	}
	if cfg.Renames.EnumValues == nil {
		cfg.Renames.EnumValues = map[string]string{}
	}
	if cfg.Overrides.Types == nil {
		cfg.Overrides.Types = map[string]Override{}
	}
	if cfg.Overrides.Columns == nil {
		cfg.Overrides.Columns = map[string]Override{}
	}
	if cfg.Messages == nil {
		cfg.Messages = map[string]MessageSpec{}
	}
	if cfg.Proto.Schemas == nil {
		cfg.Proto.Schemas = map[string]string{}
	}
	return cfg, nil
}

// OutputPackage is the proto package for a PostgreSQL schema (buf PACKAGE_VERSION_SUFFIX).
func (c *Config) OutputPackage(pgSchema string) string {
	if pkg := c.Proto.Schemas[pgSchema]; pkg != "" {
		return pkg
	}
	return c.Proto.PackagePrefix
}

// PackageDir is the directory matching a proto package (buf PACKAGE_DIRECTORY_MATCH).
func PackageDir(pkg string) string {
	return strings.ReplaceAll(strings.Trim(pkg, "."), ".", "/")
}

// OutputFile is the path of a generated proto relative to proto.out.
func (c *Config) OutputFile(pgSchema, name string) string {
	dir := PackageDir(c.OutputPackage(pgSchema))
	if dir == "" {
		return name + ".proto"
	}
	return dir + "/" + name + ".proto"
}

// GoPackage is option go_package for a PostgreSQL schema.
// go_package_prefix is the parent of the package path, unless it already ends with it.
func (c *Config) GoPackage(pgSchema string) string {
	prefix := strings.Trim(c.Proto.GoPackagePrefix, "/")
	if prefix == "" {
		return ""
	}
	dir := PackageDir(c.OutputPackage(pgSchema))
	if dir == "" || prefix == dir || strings.HasSuffix(prefix, "/"+dir) {
		return prefix
	}
	return prefix + "/" + dir
}

// ValidateProto reports whether package names satisfy buf STANDARD layout rules.
func (c *Config) ValidateProto() error {
	pkgs := map[string]struct{}{}
	if c.Proto.PackagePrefix != "" {
		pkgs[c.Proto.PackagePrefix] = struct{}{}
	}
	for _, pkg := range c.Proto.Schemas {
		if pkg == "" {
			continue
		}
		pkgs[pkg] = struct{}{}
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("proto.package_prefix is required")
	}
	for pkg := range pkgs {
		if err := ValidatePackage(pkg); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePackage reports whether pkg is a buf-lint-compatible proto package.
func ValidatePackage(pkg string) error {
	if pkg == "" {
		return fmt.Errorf("proto package is empty")
	}
	if strings.Contains(pkg, "..") || strings.HasPrefix(pkg, ".") || strings.HasSuffix(pkg, ".") {
		return fmt.Errorf("proto package %q is invalid", pkg)
	}
	for _, part := range strings.Split(pkg, ".") {
		if part == "" || part != strings.ToLower(part) {
			return fmt.Errorf("proto package %q must be lower_snake_case (buf PACKAGE_LOWER_SNAKE_CASE)", pkg)
		}
	}
	if !packageVersionSuffix.MatchString(pkg) {
		return fmt.Errorf("proto package %q must end with a version such as v1 (buf PACKAGE_VERSION_SUFFIX)", pkg)
	}
	return nil
}

// MatchGlob reports whether any name matches any path.Match pattern.
func MatchGlob(patterns []string, names ...string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		for _, n := range names {
			ok, err := pathpkg.Match(p, n)
			if err == nil && ok {
				return true
			}
		}
	}
	return false
}

// ExcludesObject reports whether schema.name or name matches tables.exclude.
func (c *Config) ExcludesObject(schema, name string) bool {
	return MatchGlob(c.Tables.Exclude, schema+"."+name, name)
}

// SelectsRelation reports whether a table/view/matview is in the generation set.
func (c *Config) SelectsRelation(schema, name string) bool {
	key := schema + "." + name
	if len(c.Tables.Include) > 0 && !MatchGlob(c.Tables.Include, key, name) {
		return false
	}
	return !c.ExcludesObject(schema, name)
}

// OmitsColumn reports whether a catalog column is excluded from generated messages.
func (c *Config) OmitsColumn(schema, relation, column string) bool {
	names := []string{
		column,
		relation + "." + column,
		schema + "." + relation + "." + column,
	}
	if MatchGlob(c.Fields.Omit, names...) {
		return true
	}
	if msg, ok := c.Messages[schema+"."+relation]; ok {
		return MatchGlob(msg.Omit, names...)
	}
	return false
}

func (c *Config) MergeFlags(exclude []string, strictTypes *bool) {
	if len(exclude) > 0 {
		c.Tables.Exclude = append(c.Tables.Exclude, exclude...)
	}
	if strictTypes != nil {
		c.Options.StrictTypes = *strictTypes
	}
}

func InitTemplate() string {
	return strings.TrimSpace(`
version: "1"

proto:
  package_prefix: "db.v1"  # package と {out}/{package}/ のパス。末尾は v1 など（buf lint）
  go_package_prefix: "github.com/example/app/gen/proto"
  out: "./proto"  # buf module root。--out より低い
  schemas: {}  # PG schema -> 別 proto package（例: public: yagish_data.v1）

options:
  jsonb_as_struct: false
  validate: true
  strict_types: false  # true なら未知型・composite でエラー（既定は google.protobuf.Any）

tables:
  include: []
  exclude: ["_*"]  # glob。relation / enum の schema.name または name

fields:
  omit: []    # glob。column / relation.column / schema.relation.column
  extra: []   # 全 message に足す mixin

messages: {}  # "schema.relation": {omit, extra}

renames:
  columns: {}
  enum_values: {}

overrides:
  types: {}
  columns: {}
`) + "\n"
}

func WriteFile(path, contents string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return os.WriteFile(path, []byte(contents), 0o644)
}
