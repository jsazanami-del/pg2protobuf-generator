package emit

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/lock"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(tmplFS, "templates/*.tmpl"))

func Render(ir *lock.IR) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range ir.Files {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "file.proto.tmpl", f); err != nil {
			return nil, fmt.Errorf("render %s: %w", f.Path, err)
		}
		out[f.Path] = buf.String()
	}
	return out, nil
}
