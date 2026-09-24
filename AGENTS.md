# Repository Guidelines

## Development environment

GRIMA is a pure-Go project. Go 1.26 or newer is required. There is no Nix shell, no
container, and no global toolchain beyond the Go distribution and `make`.

```sh
go version   # must be >= 1.26
```

Use the standard task commands for routine work:

- `make build` — build the `grima` binary for the host platform.
- `make test` — run the full test suite.
- `make vet` — run `go vet` across all packages.
- `make fmt` — format all Go sources.
- `make fmt-check` — verify formatting without edits.
- `make lint` — run `fmt-check` and `vet` together.
- `make race` — run tests with the race detector.
- `make cross` — cross-compile for linux/amd64, linux/arm64, windows/amd64, darwin/arm64.
- `make run` — build and run against `configs/grima.example.toml`.
- `make clean` — remove build artifacts.

Equivalent direct invocations, when `make` is unavailable:

```sh
go build ./cmd/grima
go test ./...
go vet ./...
gofmt -l -w .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/grima
```

**`CGO_ENABLED=0` is mandatory for release builds.** The cross-platform, dependency-free
claim depends on it. Never introduce a dependency that requires cgo.

## Repository conventions

Read `docs/design.md` before changing any interface. It is the specification; code that
disagrees with it is the bug.

Invariants that must not be broken:

1. **`internal/event` is frozen.** Adding a `Kind` is compatible. Changing the meaning of
   an existing field is not, and requires updating `docs/design.md` §2 in the same change.
2. **The fingerprint engine is single-writer.** All window state is mutated by one
   goroutine. Do not add locking to make it "thread-safe" — do not call it from a second
   goroutine.
3. **Rules are pure predicates over one event.** No cross-event state, no window
   dependency. Rules subscribe to the bus directly and must not wait for a scoring tick.
4. **Drops are counted, never silent.** Any bounded queue must increment a counter that is
   surfaced through `/healthz`.
5. **Signals with no baseline are omitted, not zeroed.** In uncalibrated mode, a
   deviation signal is *unknown*, and reporting it as `0` misrepresents unknown as benign.
6. **No ML.** No training, no pretrained model artifacts, no inference runtime. This is a
   deliberate design decision documented in `docs/plan.md` §1, not an omission.
7. **No kernel drivers, no cgo.** User-space APIs only.

Package layout and ownership: `docs/architecture.md` §14.

## Version control

Use Git. The default branch is `main`; the remote is
`https://github.com/prateekpurohit13/grima.git`.

```sh
git status --short
git diff
git log --oneline -10
```

Do not commit build artifacts, baseline JSON files, or logs. `.gitignore` covers the
standard set; extend it rather than using `git add -f`.

## Commit messages

Write commit messages using the Scoped Commits convention. Use this form for normal
commits:

```text
<subsystem>(<scope>): <short description>

[optional body]

[optional trailer(s)]
```

Put the relevant subsystem or area first. Set `<scope>` to the filename when a commit
changes a single file (for example, `repo(AGENTS.md)`). For a commit that changes
multiple files, set `<scope>` to the relevant section or module (for example, `sensor`,
`score`, or `docs`). Follow it with a concise description of the change. For changes
spanning multiple areas, prefer a broader encompassing scope; alternatively, separate
scopes with commas. Use `treewide`, `all`, or `global` for changes affecting the whole
repository.

Examples:

```text
repo(AGENTS.md): add agent workflow guidelines
sensor(filewatch): handle ReadDirectoryChangesW overflow
score(fusion): clamp entropy z-score at six sigma
docs(design): specify bus drop-policy semantics
build(makefile): add cross-compile target
```

## Documentation

Three documents, in this order of authority:

- `docs/design.md` — authoritative interfaces and semantics. Change code that disagrees
  with it, or change it in the same commit.
- `docs/architecture.md` — layer structure, dataflow, ownership map.
- `docs/plan.md` — goals, decisions, roadmap, risks.

`SKILLS.md` is the operational runbook: how to build, run, calibrate, and reproduce a
detection scenario.

## Style

Coding conventions are specified in [`SKILLS.md`](SKILLS.md) §15 and are
non-negotiable. The short version: plain names, one-line comments only where the
logic is not obvious, one concern per file, 700-line hard ceiling per file, thin
`main.go` that only wires and calls, fail-safe behavior with no silent failures,
and both happy and sad paths tested for every component.

- `gofmt` is authoritative. `make fmt-check` must pass.
- Do not add a dependency for something the standard library does. Justify any new
  dependency in the commit body and record it in `THIRD_PARTY_NOTICES.md`.

## Attribution

Any code, rule, or detection logic adapted from another project must be recorded in
`THIRD_PARTY_NOTICES.md` with its source, license, and copyright holder, in the same
commit that introduces it. This is a licensing requirement, not a courtesy.
