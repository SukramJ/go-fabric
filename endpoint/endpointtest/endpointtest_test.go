// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package endpointtest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/endpointtest"
)

// recorder is a testing.TB that records failures instead of failing.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestAssertDeviceTypeConformance(t *testing.T) {
	t.Parallel()
	topology, err := endpointtest.NewEmptySnapshotter()(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The root and the Aggregator carry no servers: every mandatory one is
	// missing, starting with Base's Descriptor.
	r := &recorder{TB: t}
	found := endpointtest.AssertDeviceTypeConformance(r, topology)
	if len(found) == 0 || len(r.errs) != len(found) || !strings.HasPrefix(r.errs[0], "device type violation: endpoint 0: missing RootNode") {
		t.Fatalf("errors = %v for %d violations", r.errs, len(found))
	}

	// Tolerating every one of them passes; a stale tolerance fails.
	tolerate := make([]string, 0, len(found)+1)
	for i := range found {
		tolerate = append(tolerate, fmt.Sprintf("%d/%s", found[i].Endpoint, found[i].Key()))
	}
	r = &recorder{TB: t}
	endpointtest.AssertDeviceTypeConformance(r, topology, append(tolerate, "5/missing Nothing")...)
	if len(r.errs) != 1 || !strings.Contains(r.errs[0], `"5/missing Nothing" no longer occurs`) {
		t.Errorf("errors = %v", r.errs)
	}

	r = &recorder{TB: t}
	if got := endpointtest.AssertDeviceTypeConformance(r, &endpoint.Topology{}); len(got) != 0 || len(r.errs) != 0 {
		t.Errorf("an empty topology: %v / %v", got, r.errs)
	}
}
