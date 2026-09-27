# modtrust

Checks whether a Go build environment actually enforces module integrity —
not whether your code has known-vulnerable dependencies, but whether the
two mechanisms that make `go build` trustworthy at all (the module proxy
and the checksum database) are actually switched on.

```
$ modtrust --no-sum
go env configuration:
  GOPROXY      https://proxy.golang.org,direct               [default]
  GOSUMDB      sum.golang.org                                [default]
  GOPRIVATE    (empty)                                       [default]
  GONOSUMDB    (empty)                                       [default]
  GONOPROXY    (empty)                                       [default]
  GOINSECURE   (empty)                                       [default]
  GOFLAGS      (empty)                                       [default]
  GOENV        /home/you/.config/go/env                      [default]

1 finding(s): 0 critical, 0 warning, 1 info

INFO     proxy-direct-fallback    GOPROXY includes 'direct' as a fallback (this is the toolchain default).
         If the configured proxy is unreachable, or returns 404/410 for a module, the go command
         silently falls back to fetching directly from the module's version control host...
```

That's a clean environment — even it gets a line, because "falls back to
VCS on proxy failure" is true by default and worth knowing. Here's the
same binary against an environment with every dangerous setting below
turned on at once (real output, `examples/demo.sh`):

```
8 finding(s): 4 critical, 3 warning, 1 info

CRITICAL sumdb-off                GOSUMDB=off: checksum database verification is disabled entirely.
CRITICAL proxy-direct-only        GOPROXY=direct: no module proxy is used at all.
CRITICAL private-wildcard         GOPRIVATE contains the pattern "*", which matches module paths well beyond a private/internal set.
CRITICAL private-wildcard         GONOSUMDB contains the pattern "*", which matches module paths well beyond a private/internal set.
WARNING  private-wildcard         GONOPROXY contains the pattern "*", which matches module paths well beyond a private/internal set.
WARNING  insecure-set             GOINSECURE is set (*): matching modules may be fetched over an insecure transport.
WARNING  mod-flag-rewrite         GOFLAGS sets -mod=mod: build commands may rewrite go.mod and go.sum automatically.
INFO     gonosumcheck-ignored     GONOSUMCHECK=1 is set in the environment but the Go toolchain does not recognize it.

(exit code: 1, would fail CI with --fail-on critical)
```

## The problem

Go's supply-chain protection rests on two mechanisms that are on by
default and that most teams never check are still on: the **module
proxy** (`GOPROXY`) and the **checksum database** (`GOSUMDB`). A plain `go
build` gives no indication when either one has been weakened:

- `GOSUMDB=off` disables independent checksum verification entirely —
  every module is trusted on first sight.
- `GOPROXY` losing its proxy and falling back to `direct` means a proxy
  outage (or a deliberately misconfigured proxy) turns into a straight
  fetch from version control, silently, mid-build.
- `GOFLAGS=-mod=mod` lets an ordinary `go build` rewrite `go.mod` and
  `go.sum` on its own — a new transitive dependency gets recorded without
  ever going through a reviewable `go get`.
- `GOPRIVATE`, `GONOSUMDB` and `GONOPROXY` patterns broader than intended
  (`*`, or a bare `*.com`) quietly exempt public modules from both the
  proxy and the checksum database.
- `GONOSUMCHECK`, a name that shows up in old blog posts and Stack
  Overflow answers, does *nothing* on a modern toolchain — and nothing
  says so.

None of this shows up in a diff, a `go.sum`, or a `go build` failure. It
shows up only if you go read `go env` — and even then, a value set once
with `go env -w` on a shared CI image or a laptop persists silently,
invisible to anyone reading the shell environment or the job definition.
**"It works on my machine" is often exactly that.**

The usual workarounds don't cover this:

- **Lockfile/dependency scanners** (`lockfile-review` and friends, `go
  mod verify`, `govulncheck`) all assume the fetch-and-verify pipeline
  itself is intact. None of them ask whether it's turned on.
- **Reading `go.sum` into a PR review** tells you what was recorded, not
  whether verification was actually enforced when it was recorded, or
  whether it's still enforced now, on this machine, in this CI job.
- **"We set GOFLAGS in CI once."** CI images get rebuilt. Laptops
  accumulate `go env -w` calls from years-old setup scripts. Nothing
  re-checks the setting after the day it was written.

This is Go-specific and distinct from generic lockfile scanning — it was
verified, not assumed, by actually flipping every one of these settings
with the real `go` binary (Go 1.27.1) and confirming what does and
doesn't get caught. See [What was verified](#what-was-verified-the-gap)
below.

## Install

```sh
go install github.com/agentscope-ai-java/modtrust/cmd/modtrust@latest
```

Or as a library:

```sh
go get github.com/agentscope-ai-java/modtrust
```

Go >= 1.21. Standard library only — no runtime dependencies, including no
`golang.org/x/mod`; go.mod/go.sum are parsed by a small hand-written
reader (see [What it does not do](#what-it-does-not-do)).

## Use

```sh
modtrust                                    # check the current directory
modtrust --dir ./services/api               # check a specific module
modtrust --verify                           # also run `go mod verify`
modtrust --json                             # machine-readable output
modtrust --fail-on critical                 # exit 1 if any critical finding fires
modtrust --fail-on sumdb-off,proxy-direct-only,private-wildcard
modtrust --expect expected-go-env.json --fail-on critical,expect-mismatch
```

**By default modtrust never fails your build** — it prints what it found
and exits `0`, same convention as `lockfile-review`. You opt into gating
CI with `--fail-on`.

### Asserting a declared configuration

A team can commit the configuration it requires and gate on drift from
it, rather than trusting that a CI image still has what it had last
month:

```json
// expected-go-env.json
{
  "GOSUMDB": "sum.golang.org",
  "GOPROXY": "https://proxy.internal.example.com,direct",
  "GOPRIVATE": "*.internal.example.com",
  "GOFLAGS": ""
}
```

```sh
modtrust --expect expected-go-env.json --fail-on expect-mismatch
```

Only the variables named in the file are asserted; anything else is left
to modtrust's own rules. An unrecognized key in the file is rejected
rather than silently ignored.

## Options

| Flag | Default | What it does |
| --- | --- | --- |
| `--dir <path>` | `.` | Module directory to check (must contain `go.mod`). |
| `--no-sum` | off | Skip the go.sum-completeness check (environment analysis only). |
| `--verify` | off | Also run `go mod verify` against the module cache and surface its result. |
| `--expect <path>` | *(none)* | JSON file declaring the expected `go env` configuration; drift becomes `expect-mismatch` findings. |
| `--json` | off | Print the full result as JSON instead of the summary. |
| `--fail-on <codes-or-severities>` | *(none)* | Comma-separated finding codes (`sumdb-off`) or severities (`critical`) that make the exit code `1`. |
| `--help` | | Print usage. |

Exit codes: `0` ran cleanly (no `--fail-on` match, or `--fail-on` empty);
`1` a finding matched `--fail-on`; `2` modtrust itself failed (bad flags,
unreadable `go.mod`, no `go` binary on `PATH`).

## What each check means

| Check | Severity | Consequence |
| --- | --- | --- |
| `sumdb-off` | critical | `GOSUMDB=off`. Every module is trusted on first sight; a compromised proxy or MITM'd fetch goes undetected. |
| `proxy-direct-only` | critical | `GOPROXY=direct`. No proxy at all — every fetch goes straight to version control, with no caching or availability guarantee. |
| `private-wildcard` | critical (GOPRIVATE/GONOSUMDB) or warning (GONOPROXY only) | A pattern of exactly `*`. Disables checksum verification (and, for GOPRIVATE, proxy use too) for literally every module, including public ones. |
| `private-broad-tld` | critical or warning | A pattern like `*.com`, `*.io`, `*.dev` — a wildcard over an entire public-facing TLD, not a scoped internal domain. |
| `private-bare-pattern` | info or warning | A bare single-label pattern (`com`, `mycompany`) with no dot or wildcard — usually harmless, flagged if it happens to spell a public TLD. |
| `proxy-direct-fallback` | info (default position) or warning (`direct` listed first) | `GOPROXY` includes `direct`. A proxy failure or 404/410 silently becomes a VCS fetch. GOSUMDB, if on, still verifies the result. |
| `proxy-off` | info | `GOPROXY` includes `off`. Fails builds closed rather than fetching — a build-availability issue, not a security one. |
| `insecure-set` | warning | `GOINSECURE` is non-empty. Permits HTTP/unverified-TLS for matching modules. Does **not** disable checksum verification on its own — a common point of confusion this tool calls out explicitly. |
| `mod-flag-rewrite` | warning | `GOFLAGS` contains `-mod=mod`. Ordinary builds can rewrite `go.mod`/`go.sum` without an explicit, reviewable `go get`. |
| `mod-flag-vendor` | info | `GOFLAGS` contains `-mod=vendor`. GOPROXY/GOSUMDB don't apply at all — the build reads `vendor/` directly. |
| `sumdb-custom` | info | `GOSUMDB` points at a non-default checksum database. Verification is active, just against a different trusted operator. |
| `gonosumcheck-ignored` | info | `GONOSUMCHECK` is set but is not a variable the modern toolchain recognizes at all. See below. |
| `sum-missing-entry` | critical | A required module has no (or an incomplete) go.sum entry. |
| `sum-local-replace` | info | A required module is replaced to a local filesystem path — correctly has no checksum, reported so it isn't mistaken for a gap. |
| `sum-stale-entry` | info | go.sum has entries for modules go.mod no longer requires. |
| `verify-failed` | critical | `go mod verify` found a module cache entry that no longer matches its recorded hash. |
| `verify-passed` | info | `go mod verify` passed. |
| `expect-mismatch` | critical | The effective configuration doesn't match a declared `--expect` value. |

## What was verified (the gap)

Before writing any code, each claim above was checked against the real Go
1.27.1 toolchain, not assumed from documentation:

- **`GONOSUMCHECK` is dead.** `go env -w GONOSUMCHECK=1` is rejected
  outright (`go: unknown go command variable GONOSUMCHECK`), and `go env
  GONOSUMCHECK` prints an empty string *even when the real process
  environment has `GONOSUMCHECK=1` set* — confirmed by exporting it and
  checking both `go env` and `go env -json`. It's a pre-modules
  (GOPATH-era) name; module-aware Go has never read it. Someone who
  copies it from an old answer gets silent no-op, both ways: setting it
  to disable checks does nothing, and setting it thinking it re-enables
  checks does nothing either. `modtrust` reads it straight from the
  process environment (bypassing `go env` entirely, since `go env` won't
  show it) specifically to catch this.
- **`go env -w` pinning a variable to its own default is invisible to
  `go env -json -changed`.** Setting `GOPROXY` to exactly
  `https://proxy.golang.org,direct` via `go env -w` does not appear in
  `-changed`'s output, because the effective value matches the built-in
  default. `modtrust` reads the on-disk `go env` config file directly
  (its path from `go env GOENV`) rather than relying on `-changed`, so an
  explicit-but-default-valued config-file entry is still reported as
  config-file-sourced, not silently folded into "default".
- **Environment variables override the config file**, confirmed by
  setting `GOSUMDB` via `go env -w` and then overriding it with a process
  environment variable — the environment wins, matching `go env`'s own
  precedence, which `modtrust`'s provenance logic mirrors exactly (see
  `internal/govars`, tested in `internal/govars/govars_test.go` against
  the real binary).
- **`GONOSUMDB`/`GONOPROXY` inherit from `GOPRIVATE`** when unset
  themselves — confirmed with `go env -w GOPRIVATE='*'` and reading back
  `go env GONOSUMDB`, which reports `*` even though `GONOSUMDB` itself was
  never set. `modtrust` reports this as `inherited-from-GOPRIVATE` rather
  than a bare default.
- **`go list -m -json all` (in `-mod=readonly`, the default mode) refuses
  outright and reports only the *first* missing `go.sum` entry it hits**,
  confirmed by stripping lines from a real, `go mod tidy`-produced
  `go.sum` one and two at a time. That's why `modtrust` does its own
  go.mod/go.sum completeness pass rather than shelling out and reporting
  whatever `go list` says first — it enumerates every gap in one pass.
- **`go mod verify` genuinely catches a tampered module cache entry.**
  `internal/verify_test.go` builds a real module, lets `go mod tidy`
  populate the real module cache, appends a comment to the cached
  `rsc.io/quote` source on disk, and confirms `go mod verify` (the real
  binary) reports the mismatch and modtrust surfaces it as a critical
  `verify-failed` finding.

## What it does not do

- **It does not scan for known CVEs.** Pair it with `govulncheck` or
  Dependabot, which answer "is this known-bad code," a different question
  from "is the fetch-and-verify pipeline itself switched on."
- **It does not resolve the full transitive module graph.** go.sum
  completeness is checked against `go.mod`'s own `require` block (direct
  and indirect) — which, for Go >= 1.17 modules using graph pruning, is
  already the complete closure of modules needed to build. It does not
  shell out to `go list -m all` or otherwise perform full minimal-version
  selection, specifically so it never needs network access and never
  risks mutating the real `go.sum` it's auditing. For a pre-1.17 module
  (no graph pruning), `go.mod` may not list the full indirect closure, so
  completeness there is a lower bound, not a guarantee — the read-only
  `go mod verify`/`go build` checks catch what this pass can't see.
- **It does not reimplement `go mod verify`.** It runs the real command
  and reports its verdict, exactly as the module-cache section of the
  spec requires.
- **`go.mod`/`go.sum` parsing is hand-written**, not
  `golang.org/x/mod/modfile` — that package is a separate module (a
  runtime dependency), which this project's stdlib-only constraint rules
  out. It handles the directive forms `go`/`require`/`replace` actually
  use in practice (single-line and parenthesized blocks, `// indirect`
  markers, quoted paths) but is not a full go.mod grammar — a `go.mod`
  using an exotic quoting form it doesn't recognize produces a parse
  error rather than a wrong answer.
- **No multi-module workspace (`go.work`) awareness.** `--dir` checks one
  module at a time.
- **`GOVCS` isn't checked.** It governs which version-control tools are
  allowed for direct fetches, a related but separate control from the
  ones named in the spec this tool was built against.
- **It makes no network calls of its own.** `go env`, `go mod verify` and
  file reads only; if `go mod verify` or a fixture's `go mod tidy` needs
  the network, that's the underlying `go` command doing it, not modtrust.

## Design

`internal/govars` isolates provenance detection (environment vs.
`go env -w` config file vs. default) behind the real `go` binary, tested
by actually writing temp `GOENV` files and setting process environment
variables — never by mocking `go env`'s output. `internal/modsum` is a
small, dependency-free go.mod/go.sum reader with its own test suite, kept
separate from the rule engine (`config.go`) so "what does go.mod say" and
"is that dangerous" don't get tangled. The CLI (`cmd/modtrust`) is a thin
flag-and-formatting layer over the library, which is the intended
integration point for anything wanting structured findings instead of
text.

## Develop

```sh
go build ./...
go vet ./...
go test -race ./...
gofmt -l .            # must print nothing
./examples/demo.sh    # real before/after output against a disposable GOENV
```

## License

MIT
