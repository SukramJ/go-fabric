// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"fmt"
	"strings"
	"time"
)

// The quiet-interval table: the minimum time between two reports of a
// quieter ("Q") attribute. The generator writes it into the definition as
// spec.Attribute.QuietInterval for every Q attribute, so the generated
// server and a self-held one read the same number; a pin lift re-reads the
// lines below. An entry that names no Q attribute of its cluster fails the
// generation.
//
// quietDefault is matter.js's QuietObservable default minimumEmitInterval
// (packages/general/src/util/Observable.ts:1383-1387 at db91886c,
// Seconds.one).
const quietDefault = time.Second

// quietIntervals are the attributes whose server sets another interval,
// by "Cluster.Attribute" (matter.js names).
var quietIntervals = map[string]struct {
	interval time.Duration
	ref      string
}{
	// PowerSourceServer's three battery figures report at most every ten
	// seconds.
	"PowerSource.BatPercentRemaining": {10 * time.Second, powerSourceQuiet},
	"PowerSource.BatTimeRemaining":    {10 * time.Second, powerSourceQuiet},
	"PowerSource.BatTimeToFullCharge": {10 * time.Second, powerSourceQuiet},
}

const powerSourceQuiet = "matter.js packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34 at db91886c"

// quietInterval returns the interval of attribute a of the generator's
// cluster: the table's entry, else [quietDefault]; zero for an attribute
// without the Q quality.
func (g *generator) quietInterval(a *element) time.Duration {
	if !a.Effective.Quality["quieter"] {
		return 0
	}
	if e, ok := quietIntervals[g.c.Name+"."+a.Name]; ok {
		return e.interval
	}
	return quietDefault
}

// checkQuiet fails for a table entry of the cluster that names no Q
// attribute of it.
func (g *generator) checkQuiet() error {
	for key := range quietIntervals {
		owner, attr, _ := strings.Cut(key, ".")
		if owner != g.c.Name {
			continue
		}
		found := false
		for _, a := range g.attributes() {
			if a.Name == attr && a.Effective.Quality["quieter"] {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("quiet interval %s names no quieter attribute", key)
		}
	}
	return nil
}

// quieter reports whether the cluster serves a Q attribute.
func (g *generator) quieter() bool {
	for _, a := range g.attributes() {
		if g.quietInterval(a) > 0 {
			return true
		}
	}
	return false
}

// durationLiteral is d as Go source over package time.
func durationLiteral(d time.Duration) string {
	if d%time.Second == 0 {
		if d == time.Second {
			return "time.Second"
		}
		return fmt.Sprintf("%d * time.Second", d/time.Second)
	}
	return fmt.Sprintf("%d * time.Millisecond", d/time.Millisecond)
}
