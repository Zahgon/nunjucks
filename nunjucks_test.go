package nunjucks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/nunjucks/js"
)

func render(t *testing.T, opts Options, src string, ctx *js.Object) (string, error) {
	t.Helper()
	env := NewEnvironment(NewFileSystemLoader([]string{filepath.Join("tests", "templates")}), opts)
	return env.RenderString(src, ctx)
}

func TestRenderString(t *testing.T) {
	ctx := js.NewObject()
	ctx.Set("name", "<World>")
	cases := []struct {
		src  string
		opts Options
		want string
	}{
		{"Hello {{ name }}", DefaultOptions(), "Hello &lt;World&gt;"},
		{"Hello {{ name }}", Options{}, "Hello <World>"},
		{"{{ [1, 2] }} {{ {} }} {{ 0.1 + 0.2 }}", DefaultOptions(), "1,2 [object Object] 0.30000000000000004"},
		{"{% for i in [3, 1, 2] | sort %}{{ loop.index }}:{{ i }} {% endfor %}", DefaultOptions(), "1:1 2:2 3:3 "},
		{"{{ not 1 == 2 }}", DefaultOptions(), "false"},
		{"{% include 'item.njk' %}", DefaultOptions(), "showing "},
	}
	for _, c := range cases {
		got, err := render(t, c.opts, c.src, ctx)
		if err != nil || got != c.want {
			t.Errorf("RenderString(%q) = %q (%v), want %q", c.src, got, err, c.want)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"{{ foo() }}", "Template render error: (unknown path)\n  Error: Unable to call `foo`, which is undefined or falsey"},
		{"\n  {{ foo.bar() }}", "Template render error: (unknown path) [Line 1, Column 12]\n  Error: Unable to call `foo[\"bar\"]`, which is undefined or falsey"},
		{"{% for %}", "Template render error: (unknown path) [Line 1, Column 8]\n  unexpected token: %}"},
		{"{{ 5 | upper }}", "Template render error: (unknown path)\n  TypeError: str.toUpperCase is not a function"},
	}
	for _, c := range cases {
		_, err := render(t, DefaultOptions(), c.src, nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("RenderString(%q) error = %v, want %q", c.src, err, c.want)
		}
	}
}

func TestRenderFile(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"base.njk":  "<{% block body %}base{% endblock %}>",
		"child.njk": "{% extends 'base.njk' %}{% block body %}[{{ super() }}]{% endblock %}",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	env := NewEnvironment(NewFileSystemLoader([]string{dir}), DefaultOptions())
	got, err := env.Render("child.njk", nil)
	if err != nil || got != "<[base]>" {
		t.Errorf("Render(child.njk) = %q (%v), want %q", got, err, "<[base]>")
	}
	_, err = env.Render("missing.njk", nil)
	if err == nil || !strings.Contains(err.Error(), "template not found: missing.njk") {
		t.Errorf("Render(missing.njk) error = %v, want template not found", err)
	}
}

func BenchmarkRender(b *testing.B) {
	env := NewEnvironment(NewFileSystemLoader(nil), DefaultOptions())
	src := "{% for i in range(100) %}{{ i | string | upper }}{% if loop.last %}!{% endif %}{% endfor %}"
	for i := 0; i < b.N; i++ {
		if _, err := env.RenderString(src, nil); err != nil {
			b.Fatal(err)
		}
	}
}
