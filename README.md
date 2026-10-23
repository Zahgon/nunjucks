# Nunjucks

[![CI][github-actions-image]][github-actions-url]

[Nunjucks](https://mozilla.github.io/nunjucks/) is a full featured
templating engine, heavily inspired by
[jinja2](https://jinja.palletsprojects.com/). This is its Go implementation:
templates render exactly as with nunjucks 3.2.4 for JavaScript, down to its
value semantics (numbers, truthiness, string methods) and error messages. View
the template language docs [here](https://mozilla.github.io/nunjucks/).

## Installation

```bash
go get github.com/mozilla/nunjucks
go install github.com/mozilla/nunjucks/cmd/nunjucks-render@latest
```

The module uses the Go standard library only.

## Usage

```go
env := nunjucks.NewEnvironment(nunjucks.NewFileSystemLoader([]string{"views"}), nunjucks.DefaultOptions())
out, err := env.Render("index.njk", ctx) // ctx is a *js.Object, for example from js.ParseJSON
```

The `nunjucks-render` command renders one template:

```bash
nunjucks-render -p views -c context.json index.njk
nunjucks-render -s '{{ name | upper }}' -c - <<< '{"name": "nunjucks"}'
```

Flags: `-p/--path DIR` (repeatable), `-c/--context FILE` (`-` for stdin),
`-s/--string`, `--no-autoescape`, `--throw-on-undefined`, `--trim-blocks`,
`--lstrip-blocks`, `--tags JSON`, `--jinja-compat` and `--dev`. The rendered
text goes to stdout; a template error is printed to stderr with exit status 1,
a usage error with exit status 2.

## Documentation

The template language documentation is in [`/docs`](/docs) and publishes to
https://mozilla.github.io/nunjucks/

## Tests

```bash
go vet ./...
go test ./...
```

## Get in touch

If you have ideas or questions, or need help, you can reach us in the [#nunjucks:mozilla.org](https://matrix.to/#/#nunjucks:mozilla.org) room on [Matrix](https://wiki.mozilla.org/Matrix).

## Want to help?

Contributions are always welcome! Before you submit an issue or pull request, please read our [contribution guidelines](CONTRIBUTING.md).

## Contributors

[![Contributors graph. Avatars provided by https://contrib.rocks](https://contrib.rocks/image?repo=mozilla/nunjucks)](https://github.com/mozilla/nunjucks/graphs/contributors)


[github-actions-image]: https://github.com/mozilla/nunjucks/actions/workflows/tests.yml/badge.svg
[github-actions-url]: https://github.com/mozilla/nunjucks/actions
