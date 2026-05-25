# Testing

Both halves of the codebase run offline. Python uses `pytest`; Go uses `go test`. Both fake out `gh` with a scripted shim so the suite never hits GitHub and never depends on the user's auth state.

## What's covered

| Suite | Where | Count | What it touches |
| --- | --- | --- | --- |
| Python | `tests/` | 139 | `cache`, `config`, `frontmatter`, `gh`, `init`, `registry_api`, `registry_server` |
| Go | colocated `*_test.go` under `cli/` | varies | `agents`, `bootstrap`, `config`, `scan`, `registry`, `tui`, `jsonout`, plus every `cli/cmd/skills-registry/*.go` handler |

Python tests live in a separate `tests/` directory. Go follows the stdlib convention and keeps `*_test.go` next to the production code (`registry.go` + `registry_test.go`, `wizard.go` + `wizard_test.go`, …).

## Running

```bash
# Python — full suite with coverage
uv run pytest -v --cov=skills_mcp --cov-report=term-missing

# Python — single file
uv run pytest -v tests/test_registry_api.py

# Python — single test
uv run pytest -v tests/test_registry_api.py::test_publish_skill_atomic_happy_path

# Go — every package
(cd cli && go test ./...)

# Go — one package, verbose
(cd cli && go test -v ./internal/registry)

# Go — one test
(cd cli && go test -v -run TestPublishHappyPath ./internal/registry)
```

The CI command for both suites combined:

```bash
uv run pytest -v --cov=skills_mcp --cov-report=term-missing
(cd cli && go vet ./... && staticcheck ./... && deadcode -test ./... && gocyclo -over 15 -ignore "_test" . && go test ./...)
```

## The `gh` shim pattern

The MCP server and the Go CLI both call out to `gh`. To keep tests deterministic and offline, both suites stub `gh` with a small script that:

1. Reads a JSON file of scripted responses (`[{key, body, exit?}, …]`).
2. Joins its argv into one string.
3. Returns the first entry whose `key` substring is found in argv.
4. Pops the matched entry so each scripted response is consumed exactly once.
5. Falls through to `sys.exit(99)` with `unexpected gh call: ...` on stderr — useful for catching missing test fixtures.

### Python (`tests/test_registry_api.py`)

Python writes the shim directly as a Python script with a shebang to the real interpreter so it works even when `PATH` is stripped:

```python
shim.write_text(textwrap.dedent(f"""\
    #!{_sys.executable}
    import json, sys, pathlib
    state = pathlib.Path({str(state)!r})
    data = json.loads(state.read_text())
    argv = " ".join(sys.argv[1:])
    for i, entry in enumerate(data):
        if entry["key"] in argv:
            ...
"""))
```

The fixture patches `registry_api.find_gh` to return the shim path so `RegistryClient` invokes it instead of the real binary.

### Go (`cli/internal/registry/registry_test.go`)

The Go shim is a `/bin/sh` wrapper that pipes a heredoc into `python3`, because Go tests can't ship an interpreter:

```go
script := fmt.Sprintf(`#!/bin/sh
state=%q
python3 - <<'PY' "$state" "$@"
import fcntl, json, os, sys
state = sys.argv[1]
argv = " ".join(sys.argv[2:])
with open(state, "r+") as f:
    fcntl.flock(f, fcntl.LOCK_EX)
    ...
PY
`, statePath)
```

Two extra mechanics on the Go side:

- **`fcntl.flock`** locks the state file so concurrent goroutines don't pop entries from under each other (the Go publish path uses 8-way parallel blob upload).
- **`/bin/sh` → `python3`** indirection means the test host needs `python3` on `PATH`. Tested macOS / Linux runners already do; CI's `ubuntu-latest` does too.

The test passes the shim path into the `Client` constructor (`Client{GH: bin, …}`), so production code never reaches `FindGH`.

## Writing a new registry test

Same recipe in both languages:

1. **Enumerate the `gh` calls** your code path makes, in order. The atomic publish path makes 7: `GET ref`, `GET commit`, `GET tree`, `POST blob` (one per file), `POST tree`, `POST commit`, `PATCH ref`.
2. **Build a fixture list of `{key, body, exit}`** where each `key` is a unique substring of the joined argv (typically the HTTP method + endpoint, e.g. `"api -X PATCH repos/x/y/git/refs/heads/main"`).
3. **Queue the fixtures** with `_enqueue(state, entries)` (Python) or `stubGH(t, entries)` (Go).
4. **Call the client method** and assert on the return value or side effects.
5. **Assert nothing was left unconsumed** if you care about extra calls — both shims `sys.exit(99)` on unexpected argv, which fails the test loudly.

Example (Python, from `tests/test_registry_api.py`):

```python
def test_publish_skill_atomic_happy_path(fake_gh: Path) -> None:
    _enqueue(fake_gh, [
        {"key": "api -X GET repos/x/y/git/ref/heads/main", "body": {"object": {"sha": "parent"}}},
        {"key": "api -X GET repos/x/y/git/commits/parent",  "body": {"tree": {"sha": "base"}}},
        {"key": "api -X GET repos/x/y/git/trees/base",      "body": {"tree": []}},
        {"key": "api -X POST repos/x/y/git/blobs",          "body": {"sha": "blob"}},
        {"key": "api -X POST repos/x/y/git/trees",          "body": {"sha": "tree"}},
        {"key": "api -X POST repos/x/y/git/commits",        "body": {"sha": "commit"}},
        {"key": "api -X PATCH repos/x/y/git/refs/heads/main", "body": {"object": {"sha": "commit"}}},
    ])
    sha = RegistryClient(repo="x/y").publish_skill("code-review", {"SKILL.md": b"# hi"})
    assert sha == "commit"
```

For retry tests, queue multiple rounds and use `exit: 1` plus a 422 / 409 body string. The Python test patches `_RETRY_BASE_DELAY_S = 0.0` so retries don't sleep.

## TUI tests

The Bubble Tea models (wizard, hub, settings, listmodel) expose accessor methods (`Completed()`, `Cancelled()`, `Repo()`, `Pushed()`, `AgentsInstalled()`, `Step()`, …) specifically so tests can drive the model with `tea.Msg` values and assert on its public state. See `cli/internal/tui/wizard_test.go` for the pattern: build the model, feed messages, call accessors. There is no Bubble Tea program loop running during the test; the `Update` method is called directly.

## Coverage

The Python suite emits a coverage report. CI runs:

```bash
uv run pytest -v --cov=skills_mcp --cov-report=xml
```

The XML is generated but not uploaded anywhere; if you want a local report:

```bash
uv run pytest --cov=skills_mcp --cov-report=term-missing
```

Go coverage isn't tracked in CI. Run it locally if you need it:

```bash
(cd cli && go test -cover ./...)
(cd cli && go test -coverprofile=cover.out ./...) && go tool cover -html=cover.out
```

## Conventions

- **One test, one behaviour.** Don't bundle "publish succeeds" and "publish retries on conflict" into one function. The shim pattern makes either case easy to isolate.
- **Fixtures over mocks.** The `gh` shim is a real binary on disk that the production code calls through `subprocess` / `exec.CommandContext`. Don't reach for `unittest.mock` to bypass it.
- **Per-file ignores.** Python tests get a relaxed naming rule set (`tests/** → ["B011", "N802", "N803", "N806"]` in `ruff.toml`). Test functions use `snake_case` regardless; the ignores cover pytest-specific patterns (uppercase fixture names, etc.).
- **Cyclomatic-complexity ceiling.** Python tests are subject to the same `max-complexity = 12` ceiling. Go test files are excluded from `gocyclo` because table-driven tests naturally explode the metric.

## Cross-references

- [Development workflow](development-workflow.md) — what CI runs and when.
- [Debugging](debugging.md) — `SKILLS_LOG_LEVEL=DEBUG`, running the MCP server inline, inspecting wizard state.
- [Systems › Registry client](../systems/registry-client.md) — the six-call atomic publish sequence the shim mimics.
- [Systems](../systems/index.md) — the modules with test counterparts.
- [Apps](../apps/index.md) — the deliverables each test suite covers.
- [Deployment](../deployment.md) — the test gate the release pipeline reuses.
