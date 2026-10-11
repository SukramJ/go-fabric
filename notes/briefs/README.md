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

Learned in the third wave (2026-10-11):

- The local `../connectedhomeip` checkout may sit at another commit than the
  harness pin (6170af84). Read chip sources with
  `git -C ../connectedhomeip show 6170af8461b10b1766044122ac83332c6d00ab20:<path>`
  and cite the pin's line numbers.
- `make reachability` after every commit on the branch, not only at the
  integration: two fix-up commits without it cost a CI round.
- A CSA case that hard-codes an endpoint (TC-EEVSE-2.2 step 9) is patched
  with a harness case edit (`edits` in the family row), which works for
  Python cases as well as YAML; the reason cites the case line.
- The app-pipe handlers must match the all-clusters app per *device*
  (`OperationalStateChange` routes "Generic" and "Oven" differently).

Learned in the fifth wave (2026-10-11):

- Session rules decide certification outcomes: the CSA Python runner and
  chip-tool share one controller identity, so a same-peer eviction on a new
  CASE kills the other client's subscriptions. Mirror matter.js's per-peer
  cap, never a stricter rule.
- An SU case needs the provider gone *before* its store is removed, a staged
  image of real size (1 MiB), and the case's own `--timeout` plus a reserve.
- `notes/handover.md` carries the state of the programme between machines;
  update it when a wave moves.

