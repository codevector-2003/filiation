# Contributing to Filiation

Thanks for taking the time to help. This is a short guide, and the rules that matter are all here.

## Before you start

- For anything larger than a bug fix, open an issue first so the approach can be agreed before you
  write the code. A rejected pull request costs you an evening, not just us.
- Blank issues are turned off. Use one of the issue templates. For a bug, include what `fil version`
  and `fil where` print.
- One logical change per pull request. Unrelated fixes belong in their own.
- `main` is protected. Every change arrives through a pull request, and it merges only when the tests
  pass on Windows, macOS and Linux.

## Building and testing

You need Go 1.26. Nothing else: `fil` is one static binary with no cgo.

```
go vet ./...
go test ./...
CGO_ENABLED=0 go build -o fil ./cmd/fil
```

All three must pass before you open a pull request; CI runs the same three on every system. If you
have `golangci-lint`, run it too. `.golangci.yml` turns the architecture's import rules into checks.

The desktop app in `desktop/` is a Go module of its own and needs a C toolchain. How to build it on
each system is in [`desktop/README.md`](desktop/README.md).

- **No network in unit tests.** Record the OpenAlex responses a test needs and replay them through a
  stub `http.RoundTripper`, as the existing tests do. A test that reaches the real service passes or
  fails with someone else's server and rate limit.
- If a test is already broken on `main`, say so in the pull request rather than working around it.

## Rules a change cannot break

These come from [`CLAUDE.md`](CLAUDE.md) and [`docs/DECISIONS.md`](docs/DECISIONS.md), where each one
has its reason.

- **Open access only.** Never download a paywalled PDF, scrape a publisher's site, or use anyone's
  institutional login. PDF links come from OpenAlex's open-access locations. This is permanent.
- **No cgo in `fil`.** It ships as one binary a researcher can double-click. The desktop shell in
  `desktop/` is the only exception, and nothing in `fil` may import it.
- **No reference parser, and no knowledge graph guessed by a language model.** Citation edges come
  from OpenAlex's resolved reference lists and are ground truth.
- **All SQL lives in `internal/store`.** `cmd/`, `internal/mcpsrv` and `internal/ui` translate a
  request, call one function in `internal/library`, and format the result. A front door that imports
  `internal/store` is a bug, and `cmd/fil/imports_test.go` fails on it.
- **One writer.** SQLite allows a single writer, and one goroutine owns all writes.
- **Every work keeps its fil ID**, and no two works share an OpenAlex ID. A shared title is reported,
  never merged.
- **Schema changes are numbered steps** in `internal/store/migrate.go`, all-or-nothing, with the
  version raised in the same commit.
- **Search ranking changes only with a better score** on both question sets in
  [`eval/retrieval`](eval/retrieval/README.md). Never look at which questions in the locked set fail.

## Design changes

- A new dependency, a new direction, or a change to one of the rules above needs a decision in
  `docs/DECISIONS.md`, agreed in the issue first. Say what was measured; a decision that rests on a
  measurement can be revisited when the measurement changes.
- Check a dependency's licence before adding it. Nothing copyleft.
- A new screen is designed before it is coded.

## Code style

- **Comment generously, and explain why.** Say why the code does what it does, especially where it
  works around an OpenAlex quirk, a SQLite behaviour, or a platform difference. The existing source
  and CI files are written this way; match them.
- `gofmt`. Table-driven tests with the standard `testing` package.
- `context.Context` is the first parameter of anything that does I/O, and cancelling it must stop the
  work.
- Wrap errors with `fmt.Errorf("...: %w", err)`. Sentinel errors live in `internal/errs` and are
  compared with `errors.Is`.
- Every external API goes through one package under `internal/sources`, so rate limiting and caching
  live in one place.

## Commits

Name the part of Filiation that changed, then say what changed, in plain words. The pull request
number goes at the end when GitHub squashes the merge:

```
Citation map: the whole library on one screen (#24)
Search filters and saved views (#33)
```

The body says why the change was made and what was checked. If a decision or a measurement is
behind it, name it (D35, spike 14).

## Use of AI

AI-assisted work is welcome, within limits. The point of these rules is that a person is accountable
for everything that lands in the repository.

**Allowed:**

- Submitting code written or assisted by an AI tool.

**Not allowed:**

- Automating commits or pull requests with AI. A person must drive the submission.
- Writing commit messages or pull request descriptions with AI.
- Using AI to open issues or discussions, or to write comments.

**Required:** review every line of AI-written code before you commit it. You submit it as your own
work, and you are expected to explain and defend each line in review.

AI tools often add a `Co-Authored-By` trailer for themselves to the commit messages they write. That
trailer is a sign the rules above were not followed: remove it, and write the message yourself.

## Licence

Filiation is licensed under Apache-2.0. By contributing, you agree that your contribution is licensed
under the same terms, and that you have the right to license it. Do not submit code you did not
write unless its licence lets it be relicensed under Apache-2.0.
