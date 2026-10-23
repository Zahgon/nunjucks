package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upstreamCase is one render of the original nunjucks test suite (tests/*.js), recorded from the JavaScript
// implementation through the reference command line: arguments, optional JSON context and the expected
// stdout, stderr and exit status. Template paths in messages are written as {templates}.
type upstreamCase struct {
	Name    string   `json:"name"`
	Args    []string `json:"args"`
	Context *string  `json:"context"`
	Stdout  string   `json:"stdout"`
	Stderr  string   `json:"stderr"`
	Code    int      `json:"code"`
}

func TestUpstreamSuite(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "upstream.json"))
	if err != nil {
		t.Fatalf("reading upstream cases: %v", err)
	}
	var cases []upstreamCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("decoding upstream cases: %v", err)
	}
	templates, err := filepath.Abs(filepath.Join("..", "..", "tests", "templates"))
	if err != nil {
		t.Fatalf("resolving the templates directory: %v", err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			args := append([]string{"-p", templates}, c.Args...)
			if c.Context != nil {
				ctxFile := filepath.Join(t.TempDir(), "ctx.json")
				if err := os.WriteFile(ctxFile, []byte(*c.Context), 0o600); err != nil {
					t.Fatalf("writing context: %v", err)
				}
				args = append([]string{"-c", ctxFile}, args...)
			}
			var stdout, stderr bytes.Buffer
			code := run(args, strings.NewReader(""), &stdout, &stderr)
			gotErr := strings.ReplaceAll(stderr.String(), templates, "{templates}")
			if code != c.Code || stdout.String() != c.Stdout || gotErr != c.Stderr {
				t.Errorf("render %q\n got (exit %d) stdout %q stderr %q\nwant (exit %d) stdout %q stderr %q",
					c.Args[len(c.Args)-1], code, stdout.String(), gotErr, c.Code, c.Stdout, c.Stderr)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"a", "b"},
		{"--bogus", "x"},
		{"-p"},
		{"-c", filepath.Join(t.TempDir(), "missing.json"), "x"},
		{"--tags", "[1]", "-s", "x"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
			t.Errorf("run(%q) = exit %d, want 2 (stderr %q)", args, code, stderr.String())
		}
		if !strings.HasPrefix(stderr.String(), "nunjucks-render: ") || stdout.Len() != 0 {
			t.Errorf("run(%q): stdout %q stderr %q, want only a nunjucks-render: usage line", args, stdout.String(), stderr.String())
		}
	}
}

func TestContextFromStdin(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-c", "-", "-s", "{{ b }}{{ a }}{% for k, v in o %}{{ k }}{% endfor %}"},
		strings.NewReader(`{"a":1,"b":"x","o":{"z":1,"2":2,"1":3}}`), &stdout, &stderr)
	if code != 0 || stdout.String() != "x112z" {
		t.Errorf("render with stdin context: exit %d, stdout %q, stderr %q; want exit 0 and %q", code,
			stdout.String(), stderr.String(), "x112z")
	}
}
