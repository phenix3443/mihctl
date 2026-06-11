package configgen

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

type RenderData struct {
	ExternalUI      string
	Tun             map[string]any
	Template        TemplateSpec
	Proxies         []any
	ProxyGroups     OrderedList
	ProxyGroupNames map[string]bool
	ProxyProviders  OrderedMap
	RuleProviders   OrderedMap
	Rules           []string
}

func RenderTemplate(path string, data RenderData) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}

	tmpl, err := template.New(filepathBase(path)).Funcs(template.FuncMap{
		"toYAML": func(value any) (string, error) {
			out, err := yaml.Marshal(value)
			if err != nil {
				return "", err
			}
			return strings.TrimRight(string(out), "\n"), nil
		},
		"indent": func(spaces int, value string) string {
			prefix := strings.Repeat(" ", spaces)
			lines := strings.Split(value, "\n")
			for index := range lines {
				if lines[index] == "" {
					continue
				}
				lines[index] = prefix + lines[index]
			}
			return strings.Join(lines, "\n")
		},
	}).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", path, err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", path, err)
	}
	return buffer.String(), nil
}

func filepathBase(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}
