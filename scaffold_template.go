package fs

import (
	"bytes"
	"text/template"
)

// renderTemplate parses src as a [text/template] and executes it
// with vars. Used for both source paths (so a template name like
// `{{.AppName}}.go` resolves to `myapp.go`) and file contents
// (substitution into the rendered output).
//
// Templates have NO custom functions installed; text/template's
// default builtins only. Avoiding e.g. `os.Exec`-exposing helpers
// means a malicious template in a scaffolding source can only
// transform the input variables, not invoke shell commands.
func renderTemplate(src string, vars any) (string, error) {
	tmpl, err := template.New("rotini-scaffold").Option("missingkey=error").Parse(src)
	if err != nil {
		return "", err //nolint:wrapcheck // outer caller wraps via *PathError
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err //nolint:wrapcheck // outer caller wraps via *PathError
	}
	return buf.String(), nil
}
