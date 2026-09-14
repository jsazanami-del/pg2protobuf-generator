package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = "1"

// Config is .pg2proto.yaml.
type Config struct {
	Version   string    `yaml:"version"`
	Proto     Proto     `yaml:"proto"`
	Options   Options   `yaml:"options"`
	Tables    Tables    `yaml:"tables"`
	Renames   Renames   `yaml:"renames"`
	Overrides Overrides `yaml:"overrides"`
}

type Proto struct {
	PackagePrefix   string `yaml:"package_prefix"`
	GoPackagePrefix string `yaml:"go_package_prefix"`
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
			PackagePrefix: "db.v1",
		},
		Options: Options{
			Validate: true,
		},
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
	return cfg, nil
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
  package_prefix: "db.v1"
  go_package_prefix: "github.com/example/app/gen/proto/db/v1"

options:
  jsonb_as_struct: false
  validate: true
  strict_types: false  # true なら未知型・composite でエラー（既定は google.protobuf.Any）

tables:
  include: []
  exclude: ["_*"]

renames:
  columns: {}
  enum_values: {}

overrides:
  types: {}
  columns: {}
`) + "\n"
}

func WriteFile(path string, contents string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return os.WriteFile(path, []byte(contents), 0o644)
}
