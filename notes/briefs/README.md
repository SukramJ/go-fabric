# Implementation briefs

A brief is a finished plan for one implementing agent: scope, the facts it
rests on (each with its source), the files it owns, the guards that accept
it, and what it must not touch. Briefs exist so that the model rule in
`CLAUDE.md` ("Which model does which work") holds: an implementing agent
transcribes the facts a brief gives it and derives none of its own. A fact a
brief does not carry is a question back to the owner, not a guess.

Rules for every brief:

- Branch from `origin/main`, one PR per brief, never a push to `main`.
- `make ci` green locally before the PR; the chip-tool jobs run in CI.
- A `// Mirrors matter.js …` comment quotes the path the brief gives;
  `notes/parity/by_design.md` and the findings register are not edited by
  the agent — a needed entry is listed in the PR description for the owner.
- CHANGELOG entry under `[Unreleased]` for user-visible changes.
- The PR description lists: what was built, which guard proves it, what was
  left out and why, and every open question.

Learned in the first wave (2026-10-10):

- A new endpoint in the reference daemon needs a CI fixture run
  (`chiptool.yml` dispatch with `update_fixtures=true`) whose family list
  includes **IDM** besides the new families: IDM/10.4 runs per endpoint and
  the golden needs its entry for every new endpoint, or the core group fails
  on "passed but has no entry".
- A device type a family row names must also be in the harness's
  device-type map (`familyrun_test.go`, the `epOf` list), or every case of
  the family fails with "no endpoint advertises device type".
- Waves are integrated locally onto one branch and opened as one PR
  (owner's rule, 2026-10-10): the agents keep their own branches and
  worktrees; the owner rebases them in sequence, regenerates the
  reachability inventory instead of merging it, runs `make ci` once and
  opens the PR with auto-merge.
