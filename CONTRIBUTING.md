# Contributing to go-fabric

`go-fabric` is a library: a pure-Go implementation of the device side of the
Matter protocol that a host application embeds. Everything below follows from
that — the module has no binary, no configuration file and no operator
surface, and its public API is the whole of its contract.

## Before you open a PR

- Read [`CLAUDE.md`](./CLAUDE.md). matter.js HEAD is the gold standard for
  every Matter-side decision, and the workflow for citing it is written down
  there.
- Open an issue for any non-trivial change so the shape is agreed before the
  code lands.
- Sign off every commit (`git commit -s`) — DCO applies, so the authorship
  chain stays verifiable.

## Local setup

```sh
git clone https://github.com/SukramJ/go-fabric
cd go-fabric
go build ./...
go test ./...
```

Prerequisites:

- Go 1.27+ (`go.mod` pins the language version; CI installs the same
  toolchain it declares).
- `gofumpt` and `golangci-lint` **v2** — a v1 binary rejects this repo's
  v2-format `.golangci.yaml`. Install the versions CI pins, which are the
  `GOFUMPT_VERSION` and `GOLANGCI_LINT_VERSION` values in
  [`.github/workflows/ci.yml`](./.github/workflows/ci.yml):

  ```sh
  go install mvdan.cc/gofumpt@<GOFUMPT_VERSION>
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<GOLANGCI_LINT_VERSION>
  ```

  Both are pinned deliberately. A new lint release adds checks that are style
  decisions, and floating on `@latest` makes the gate report on the calendar
  rather than on the diff. Bumping one is its own change, with whatever the
  new release finds fixed or justified in the same commit.

`make help` lists the targets; `make ci` runs what the pipeline runs.

## The chip-tool suite

`internal/chiptool` drives the CSA reference commissioner against
`examples/reference-bridge` over real PASE and CASE. It needs Linux, a
chip-tool and a running avahi-daemon, and runs locally with no exports:

```sh
make chiptool-setup   # once: connectedhomeip at the pin (../connectedhomeip) + the YAML-runner venv
make chiptool-test
```

chip-tool comes from `$GOFABRIC_CHIPTOOL_BIN`, `./bin/chip-tool` or PATH, in
that order. On arm64, `make chiptool-extract` copies the CI pin out of the
chip-cert-bins image; that image has no amd64 build, so on amd64 install the
snap (`sudo snap install chip-tool`). Every run logs which chip-tool it used.
`internal/chiptool/doc.go` explains the rest, including what differs between
the snap and the CI pin.

## What a passing PR looks like

- `go build ./...` and `go vet ./...` clean.
- `go test -race ./...` green. The race detector is not optional here: the
  session, subscription and MRP paths are concurrent by construction, and a
  race in them surfaces as an intermittent pairing failure in a controller
  nobody can attach a debugger to. `-race` needs cgo, so run it with
  `CGO_ENABLED=1`; everything else builds with cgo off.
- `gofumpt -l .` prints nothing and `golangci-lint run ./...` reports zero
  findings.
- `go mod tidy -diff` prints nothing.
- User-visible changes have an entry in [`CHANGELOG.md`](./CHANGELOG.md)
  under `[Unreleased]`.
- Every new `.go` file opens with the MIT header — `TestLicenseHeaderOnEveryGoFile`
  fails naming the file otherwise:

  ```go
  // SPDX-License-Identifier: MIT
  // Copyright (C) 2026 SukramJ.
  ```

## Matter-side changes

Cluster IDs, revisions, attribute IDs, constraints, defaults and wire shape
come verbatim from matter.js HEAD — hand-coding any of them from the
specification is what drift is made of, and drift shows up as a silent
Apple/Google pair-abort rather than a failing test. Cite the matter.js path
and function in the Go code, and extend the parity tests
(`parity_matterjs_test.go`, the embedded schema under `parity/`) in the same
change.

## Dependencies

Five direct requires, listed in `go.mod` and recorded with their licences in
[`THIRD-PARTY-NOTICES.md`](./THIRD-PARTY-NOTICES.md). Adding a sixth is a
decision, not a convenience:

- **No cgo.** The module builds with `CGO_ENABLED=0`; only the race-enabled
  test run turns it on.
- **MIT / Apache-2.0 / BSD only.** GPL, LGPL, MPL and AGPL pull in copyleft
  obligations the MIT licence of this module cannot carry — stop and discuss.
- A new dependency updates `THIRD-PARTY-NOTICES.md` and, when its licence
  requires the text to travel, adds it under `licenses/`.

## Versioning

The module has an independent SemVer lane; `v0.1.0` is its first tag. A
merge to `main` is still immediately consumable by anyone who runs
`go get github.com/SukramJ/go-fabric@main`, so treat `main` accordingly. A
release is a commit that turns `## [Unreleased]` into a dated
`## [x.y.z]` section, followed by the `vx.y.z` tag on that commit; the
release workflow refuses a tag without its CHANGELOG section.
