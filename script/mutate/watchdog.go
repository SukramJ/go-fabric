// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"time"
)

// rssEvery is the watchdog's poll interval. A runaway mutant was measured
// growing by about 2 GB/s (review II phase 8, w2-b2b: subscribe_dispatch.go
// :229 guard off, 39 MB → 6 GB in 3 s), so a poll overshoots the limit by
// at most some 400 MB.
const rssEvery = 200 * time.Millisecond

// watchRSS polls the resident memory of process group pgid until done is
// closed; above limit bytes it kills the group, waits for it to be gone and
// reports true. A failed read is not over the limit.
func watchRSS(pgid int, limit int64, done <-chan struct{}) bool {
	t := time.NewTicker(rssEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return false
		case <-t.C:
			if rss, err := groupRSS(pgid); err == nil && rss > limit {
				killGroup(pgid)
				return true
			}
		}
	}
}

// parseProcStat reads the process group and the resident set, in pages,
// from the text of a Linux /proc/<pid>/stat file: proc(5) numbers the
// fields from 1 — (2) comm in parentheses, (5) pgrp, (24) rss — and comm
// may itself hold spaces and parentheses, so the fields are counted from
// the last ')'.
func parseProcStat(b []byte) (pgrp int, rssPages int64, ok bool) {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0, 0, false
	}
	f := bytes.Fields(b[i+1:]) // f[0] is field 3
	if len(f) < 22 {
		return 0, 0, false
	}
	g, err1 := strconv.Atoi(string(f[2]))
	r, err2 := strconv.ParseInt(string(f[21]), 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return g, r, true
}

// statGroupPages sums, in pages, the resident set of the processes of group
// pgid from the Linux /proc/<pid>/stat files at paths. A file that cannot be
// read — its process exited between the listing and the read — or does not
// parse is skipped.
func statGroupPages(paths []string, pgid int) int64 {
	var pages int64
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // a /proc stat file
		if err != nil {
			continue
		}
		if g, r, ok := parseProcStat(b); ok && g == pgid {
			pages += r
		}
	}
	return pages
}

// sumPsGroupRSS sums, in bytes, the rss column of the lines of
// `ps -A -o pgid=,rss=` whose pgid is pgid; ps(1) gives rss in units of
// 1024 bytes. Lines that do not parse are skipped.
func sumPsGroupRSS(out []byte, pgid int) int64 {
	var sum int64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := bytes.Fields(sc.Bytes())
		if len(f) != 2 {
			continue
		}
		g, err1 := strconv.Atoi(string(f[0]))
		kib, err2 := strconv.ParseInt(string(f[1]), 10, 64)
		if err1 == nil && err2 == nil && g == pgid {
			sum += kib << 10
		}
	}
	return sum
}
