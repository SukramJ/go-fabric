// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package requiredchecks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// workflow is what the guard reads from one file under .github/workflows:
// the events that start it and its jobs.
type workflow struct {
	file   string
	events []string
	jobs   []job
}

// job is one entry under `jobs:`. name is the job's `name:` as written (a
// matrix job's is its template, unexpanded); a job without one is known by
// its id, which is this guard's key for it and not a statement about the
// check name GitHub derives.
type job struct {
	id   string
	name string
	line int
}

// key is the name a table row holds the job by.
func (j job) key() string {
	if j.name != "" {
		return j.name
	}
	return j.id
}

var (
	topKey   = regexp.MustCompile(`^("?)([A-Za-z_][\w-]*)("?):(.*)$`)
	jobKey   = regexp.MustCompile(`^ {2}([A-Za-z_][\w-]*):\s*(?:#.*)?$`)
	eventKey = regexp.MustCompile(`^ {2}([a-z_]+):`)
	eventSeq = regexp.MustCompile(`^ {2}- ([a-z_]+)\s*(?:#.*)?$`)
	jobField = regexp.MustCompile(`^ {4}([A-Za-z_][\w-]*):(.*)$`)
)

// parseWorkflow reads the three things the guard needs and nothing else.
// The module carries no YAML dependency (five direct dependencies, each a
// decision), so this is a line reader for the shape the workflows are
// written in — two-space indentation, `on:` and `jobs:` as top-level keys,
// one job per two-space key, its `name:` at four — and it refuses every
// form it does not model rather than reading past it: a tab, an odd
// indentation under `jobs:`, a `name:` that is a block scalar, a job that
// calls a reusable workflow (its checks are named "caller / callee").
func parseWorkflow(file, src string) (workflow, error) {
	wf := workflow{file: file}
	section := ""
	sawOn, sawJobs := false, false
	for n, raw := range strings.Split(src, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(line[indent:], "\t") {
			return wf, fmt.Errorf("%s:%d: a tab in the indentation", file, n+1)
		}
		if indent == 0 {
			m := topKey.FindStringSubmatch(line)
			if len(m) != 5 || m[1] != m[3] {
				return wf, fmt.Errorf("%s:%d: not a top-level key: %q", file, n+1, line)
			}
			section = m[2]
			switch section {
			case "on":
				sawOn = true
				events, err := inlineEvents(m[4])
				if err != nil {
					return wf, fmt.Errorf("%s:%d: %w", file, n+1, err)
				}
				wf.events = append(wf.events, events...)
			case "jobs":
				sawJobs = true
				if rest := stripComment(m[4]); rest != "" {
					return wf, fmt.Errorf("%s:%d: jobs: with an inline value %q", file, n+1, rest)
				}
			}
			continue
		}
		switch section {
		case "on":
			if indent != 2 {
				continue // an event's own configuration
			}
			if m := eventKey.FindStringSubmatch(line); m != nil {
				wf.events = append(wf.events, m[1])
			} else if m := eventSeq.FindStringSubmatch(line); m != nil {
				wf.events = append(wf.events, m[1])
			} else {
				return wf, fmt.Errorf("%s:%d: not an event under on: %q", file, n+1, line)
			}
		case "jobs":
			switch indent {
			case 2:
				m := jobKey.FindStringSubmatch(line)
				if m == nil {
					return wf, fmt.Errorf("%s:%d: not a job key: %q", file, n+1, line)
				}
				wf.jobs = append(wf.jobs, job{id: m[1], line: n + 1})
			case 1, 3:
				return wf, fmt.Errorf("%s:%d: indentation %d under jobs", file, n+1, indent)
			case 4:
				if len(wf.jobs) == 0 {
					return wf, fmt.Errorf("%s:%d: a job field before a job", file, n+1)
				}
				m := jobField.FindStringSubmatch(line)
				if m == nil {
					continue // a continuation line of a job-level value
				}
				cur := &wf.jobs[len(wf.jobs)-1]
				switch m[1] {
				case "uses":
					return wf, fmt.Errorf("%s:%d: job %s calls a reusable workflow; its check names are not modelled", file, n+1, cur.id)
				case "name":
					if cur.name != "" {
						return wf, fmt.Errorf("%s:%d: job %s has two names", file, n+1, cur.id)
					}
					name, err := scalar(m[2])
					if err != nil {
						return wf, fmt.Errorf("%s:%d: job %s: %w", file, n+1, cur.id, err)
					}
					cur.name = name
				}
			}
		}
	}
	if !sawOn {
		return wf, fmt.Errorf("%s: no on: key", file)
	}
	if !sawJobs || len(wf.jobs) == 0 {
		return wf, fmt.Errorf("%s: jobs: holds no job", file)
	}
	if len(wf.events) == 0 {
		return wf, fmt.Errorf("%s: on: names no event", file)
	}
	sort.Strings(wf.events)
	return wf, nil
}

// inlineEvents reads the value on the `on:` line itself: nothing (a
// mapping or a sequence follows), one event, or a flow sequence.
func inlineEvents(rest string) ([]string, error) {
	rest = stripComment(rest)
	if rest == "" {
		return nil, nil
	}
	if strings.HasPrefix(rest, "[") {
		if !strings.HasSuffix(rest, "]") {
			return nil, fmt.Errorf("on: with an unclosed flow sequence %q", rest)
		}
		var out []string
		for _, e := range strings.Split(rest[1:len(rest)-1], ",") {
			e = strings.TrimSpace(e)
			if !regexp.MustCompile(`^[a-z_]+$`).MatchString(e) {
				return nil, fmt.Errorf("on: with the event %q", e)
			}
			out = append(out, e)
		}
		return out, nil
	}
	if !regexp.MustCompile(`^[a-z_]+$`).MatchString(rest) {
		return nil, fmt.Errorf("on: with the value %q", rest)
	}
	return []string{rest}, nil
}

// scalar reads a one-line YAML scalar: plain, single-quoted or
// double-quoted without escapes.
func scalar(rest string) (string, error) {
	rest = strings.TrimSpace(rest)
	switch {
	case rest == "":
		return "", errors.New("an empty name")
	case rest[0] == '|' || rest[0] == '>':
		return "", errors.New("a block scalar as name")
	case rest[0] == '"':
		end := strings.Index(rest[1:], `"`)
		if end < 0 || strings.Contains(rest[1:1+end], `\`) || stripComment(rest[end+2:]) != "" {
			return "", fmt.Errorf("a double-quoted name this reader does not model: %s", rest)
		}
		return rest[1 : 1+end], nil
	case rest[0] == '\'':
		end := strings.LastIndex(rest, `'`)
		if end == 0 || strings.Contains(rest[1:end], `'`) || stripComment(rest[end+1:]) != "" {
			return "", fmt.Errorf("a single-quoted name this reader does not model: %s", rest)
		}
		return rest[1:end], nil
	}
	return stripComment(rest), nil
}

// stripComment drops a trailing " # …" comment of a plain value.
func stripComment(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return ""
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// loadWorkflows parses every workflow of the repository.
func loadWorkflows(t *testing.T) []workflow {
	t.Helper()
	dir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []workflow
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		wf, err := parseWorkflow(e.Name(), string(raw))
		if err != nil {
			t.Fatalf("the workflow reader does not model this file; extend it rather than the tables: %v", err)
		}
		out = append(out, wf)
	}
	return out
}

// TestParseWorkflow holds the reader to the forms it models and to a
// refusal of the ones it does not.
func TestParseWorkflow(t *testing.T) {
	good := `name: x
# yamllint disable-line rule:truthy
on:
  pull_request:
    branches: [main]
  schedule:
    - cron: "1 2 * * *"
env:
  A: "1"
jobs:
  # a comment
  first:
    name: first job (linux) # trailing
    if: |
      a ||
      b
    steps:
      - name: not a job name
  second:
    # no name
    runs-on: x
  third:
    name: "quoted: name"
  fourth:
    name: families (${{ matrix.group }})
`
	wf, err := parseWorkflow("x.yml", good)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wf.events, ","); got != "pull_request,schedule" {
		t.Errorf("events = %s", got)
	}
	keys := make([]string, 0, len(wf.jobs))
	for _, j := range wf.jobs {
		keys = append(keys, j.key())
	}
	want := "first job (linux)|second|quoted: name|families (${{ matrix.group }})"
	if got := strings.Join(keys, "|"); got != want {
		t.Errorf("jobs = %s, want %s", got, want)
	}
	for _, on := range []struct{ src, events string }{
		{"on: pull_request_target\njobs:\n  a:\n    runs-on: x\n", "pull_request_target"},
		{"on: [push, pull_request]\njobs:\n  a:\n    runs-on: x\n", "pull_request,push"},
		{"on:\n  - push\njobs:\n  a:\n    runs-on: x\n", "push"},
		{"\"on\":\n  push:\njobs:\n  a:\n    runs-on: x\n", "push"},
	} {
		wf, err := parseWorkflow("x.yml", on.src)
		if err != nil {
			t.Errorf("%q: %v", on.src, err)
			continue
		}
		if got := strings.Join(wf.events, ","); got != on.events {
			t.Errorf("%q: events = %s, want %s", on.src, got, on.events)
		}
	}
	for name, src := range map[string]string{
		"tab":               "on: push\njobs:\n\ta:\n",
		"odd indentation":   "on: push\njobs:\n   a:\n    runs-on: x\n",
		"reusable workflow": "on: push\njobs:\n  a:\n    uses: ./.github/workflows/x.yml\n",
		"block scalar name": "on: push\njobs:\n  a:\n    name: >\n      x\n",
		"escaped name":      "on: push\njobs:\n  a:\n    name: \"a\\\"b\"\n",
		"no jobs":           "on: push\njobs:\n",
		"no on":             "jobs:\n  a:\n    runs-on: x\n",
		"flow mapping on":   "on: {push: {}}\njobs:\n  a:\n    runs-on: x\n",
		"inline jobs":       "on: push\njobs: {a: {}}\n",
		"two names":         "on: push\njobs:\n  a:\n    name: x\n    name: y\n",
	} {
		if _, err := parseWorkflow("x.yml", src); err == nil {
			t.Errorf("%s: the reader accepted a form it does not model", name)
		}
	}
}
