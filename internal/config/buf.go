package config

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const bufFileName = "buf.yaml"

type bufYAML struct {
	Version string `yaml:"version"`
	Modules []struct {
		Path string `yaml:"path"`
	} `yaml:"modules"`
}

// ResolveBufModule walks from startDir toward the filesystem root looking for
// buf.yaml. When found, it selects a v2 module and returns its directory.
// ok is false only when no buf.yaml exists; parse and selection errors return err.
func ResolveBufModule(startDir, want string) (out string, ok bool, err error) {
	bufPath, err := findBufYAML(startDir)
	if err != nil {
		return "", false, err
	}
	if bufPath == "" {
		return "", false, nil
	}
	mods, err := loadBufModules(bufPath)
	if err != nil {
		return "", false, err
	}
	selected, err := selectBufModule(mods, want)
	if err != nil {
		return "", false, err
	}
	dir := filepath.Dir(bufPath)
	resolved := dir
	if selected != "." {
		resolved = filepath.Join(dir, filepath.FromSlash(selected))
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", false, err
	}
	return abs, true, nil
}

func findBufYAML(startDir string) (string, error) {
	if startDir == "" {
		startDir = "."
	}
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, bufFileName)
		st, err := os.Stat(candidate)
		if err == nil {
			if st.IsDir() {
				return "", fmt.Errorf("%s is a directory", candidate)
			}
			return candidate, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func loadBufModules(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc bufYAML
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Version != "v2" {
		return nil, fmt.Errorf("%s version %q is unsupported (need v2)", path, doc.Version)
	}
	if doc.Modules == nil {
		return []string{"."}, nil
	}
	if len(doc.Modules) == 0 {
		return nil, fmt.Errorf("%s has no modules", path)
	}
	mods := make([]string, 0, len(doc.Modules))
	seen := map[string]struct{}{}
	for _, m := range doc.Modules {
		p := normalizeModulePath(m.Path)
		if _, ok := seen[p]; ok {
			return nil, fmt.Errorf("%s has duplicate module path %q", path, p)
		}
		seen[p] = struct{}{}
		mods = append(mods, p)
	}
	return mods, nil
}

func selectBufModule(mods []string, want string) (string, error) {
	want = strings.TrimSpace(want)
	if want != "" {
		want = normalizeModulePath(want)
	}
	if want == "" {
		uniq := uniqueStrings(mods)
		if len(uniq) == 1 {
			return uniq[0], nil
		}
		return "", fmt.Errorf("buf.yaml has multiple modules; set proto.module or --module to one of: %s", strings.Join(uniq, ", "))
	}
	for _, m := range mods {
		if m == want {
			return m, nil
		}
	}
	return "", fmt.Errorf("buf.yaml has no module matching %q; available: %s", want, strings.Join(uniqueStrings(mods), ", "))
}

func normalizeModulePath(p string) string {
	p = strings.TrimSpace(p)
	p = filepath.ToSlash(p)
	if p == "" || p == "." || p == "./" {
		return "."
	}
	p = pathpkg.Clean(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" || p == "." {
		return "."
	}
	return p
}

func uniqueStrings(in []string) []string {
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
