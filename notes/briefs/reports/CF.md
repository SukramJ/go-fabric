# Report CF — M4 phase 22: the two repositories (go-habridge, go-hafabric)

Date 2026-10-24. Owner wave (concept v7 B.4, decisions 0.4 and 0.6; owner
decisions of 2026-10-24: start CF parallel to CE, `CONCEPT.md` on the state
of concept v8 — `go-fabric-server` as an own repository, go-habridge as its
thin client, the controller role later in the same daemon).

## Repositories

| Repository | Description | Status |
| --- | --- | --- |
| [SukramJ/go-habridge](https://github.com/SukramJ/go-habridge) | Home Assistant entities as a bridge for Matter controllers, in pure Go on go-fabric | concept, not started |
| [SukramJ/go-hafabric](https://github.com/SukramJ/go-hafabric) | Home Assistant component to Matter cluster mapping for go-fabric bridges | concept, not started |

Each holds: `README.md` (name, badges, purpose, twin sentence, status
block, the paragraph "Why "Matter" is not in the name", Features /
Quickstart / Configuration / Development / Acknowledgements / License),
`CONCEPT.md` (English, from concept v6 part B / C on the v8 decisions),
`LICENSE` (Apache-2.0, go-fabric's), `CLA.md` (go-fabric's, project name
replaced), `AI_POLICY.md` (go-zendure2mqtt's, project name and the gate
sentence replaced), `CLAUDE.md` (short: what it is, twin, model rules as
go-fabric's, the naming rule), `changelog.md` with `[Unreleased]`,
`.gitignore`, `.golangci.yaml` (go-zendure2mqtt's, module path replaced),
`Makefile` (build / vet / test / race / fmt / lint / tidy-check / vuln /
licenses / ci), `go.mod` (go 1.27.2) with a `doc.go`; `.github/dependabot.yml`
(gomod + github-actions, weekly, go-zendure2mqtt's), `workflows/ci.yml`
(lint, security, test matrix, build; coverage and fuzz-smoke present with
`if: false` until the first code), `codeql.yml` (go-zendure2mqtt's),
`dependabot-auto-merge.yml` (non-major), `release-on-tag.yml` (`if: false`
until the first release). go-habridge additionally: `cmd/habridge/main.go`
(a placeholder that states the status and exits 2), `Dockerfile`
(distroless, non-root), `addon/README.md` (placeholder),
`docker-build-push.yml` and `addon-image.yml` (`if: false`).

## Acceptance (the `gh` outputs, 2026-10-24)

```
{"default":"main","delBranch":true,"desc":"Home Assistant entities as a bridge for Matter controllers, in pure Go on go-fabric","disc":false,"home":"","issues":true,"license":"apache-2.0","merge":false,"projects":false,"rebase":false,"squash":true,"topics":["go","golang","home-assistant","homeassistant","smart-home","iot","matter","matter-bridge","home-automation"],"visibility":"PUBLIC","wiki":false}
{"admins":false,"checks":["build","lint","test (ubuntu-latest)","test (macos-latest)","test (windows-latest)","codeql (go)","security (vuln + licenses + secrets)"],"conv":true,"del":false,"force":false,"reviews":0,"strict":true}
accessibility, bug, dependencies, documentation, duplicate, enhancement, github-actions, github_actions, go, good first issue, help wanted, invalid, needs-integration, question, wontfix
{"default":"main","delBranch":true,"desc":"Home Assistant component to Matter cluster mapping for go-fabric bridges","disc":false,"home":"","issues":true,"license":"apache-2.0","merge":false,"projects":false,"rebase":false,"squash":true,"topics":["go","golang","home-assistant","homeassistant","smart-home","iot","matter","matter-bridge","home-automation","library"],"visibility":"PUBLIC","wiki":false}
{"admins":false,"checks":["build","lint","test (ubuntu-latest)","test (macos-latest)","test (windows-latest)","codeql (go)","security (vuln + licenses + secrets)"],"conv":true,"del":false,"force":false,"reviews":0,"strict":true}
accessibility, bug, dependencies, documentation, duplicate, enhancement, github-actions, github_actions, go, good first issue, help wanted, invalid, needs-integration, question, wontfix
```

Settings as go-zendure2mqtt's: public, issues on, wiki / projects /
discussions off, squash only, delete branch on merge, default `main`,
licence Apache-2.0 (the one intended deviation). Branch protection:
required checks strict (`build`, `lint`, `test (ubuntu-latest)`,
`test (macos-latest)`, `test (windows-latest)`, `codeql (go)`,
`security (vuln + licenses + secrets)`), conversation resolution, no
force pushes, no deletions, reviews 0, admins not enforced. `coverage`
and `fuzz-smoke` join the required set with the first code. Labels: the
GitHub set plus `dependencies`, `github_actions`, `github-actions`, `go`,
`needs-integration`.

## go-fabric changes (this pull request)

- Concept v6 moved to `docs/history/` (phase 20 is executed; v7 is the
  live concept); every reference repointed.
- Roadmap consumer table: the two rows name the repositories, their state
  and what they need; the milestone-4 paragraph says the concepts moved.
- Concept v6 parts B and C carry a note with the link to their new home.
- `docs/matterjs-comparison.md` rows 139 (`cli-tool`, `nodejs-shell`) and
  140 (`mqtt`, `react-native`, `nodejs-ws`) stay ○ with their reasons; the
  remote protocol they would compare to is `go-fabric-server`'s, after
  milestone 4.

## Differences from the checklist

- `repository.yaml` and a real `addon/` are not committed: a Home
  Assistant add-on repository entry for an image that does not exist
  would offer users a broken install. `addon/README.md` says when it
  arrives.
- The gitleaks step of the `security` job joins with the first code that
  handles a credential; the job name and its required-check context are
  already the final ones.
- The `go` label is added (Dependabot's gomod group labels with it).
- The checklist lacked one setting the reference repository has: **allow
  auto-merge** (`allow_auto_merge`). Without it the `Dependabot auto-merge`
  workflow fails on the first Dependabot pull request (`Auto merge is not
  allowed for this repository`), which it did on both repositories.
  Enabled on both; the two Dependabot pull requests are on auto-merge;
  the checklist in concept v7 B.4 names it now.

## Left out

- Pages sites, homepages: empty until there is a site.
- Anything that runs: nothing does; both repositories state it.

## Questions

None.
