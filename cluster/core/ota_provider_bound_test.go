// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"encoding/binary"
	"errors"
	"testing"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	"github.com/SukramJ/go-fabric/im"
)

// TestOTAProvider_InProgressBoundedPerFabric (review II, R2-P6-bound-1206,
// decision 134): ApplyUpdateRequests with tokens the provider never issued
// still answer as matter.js does (an unknown token matches any version),
// but one fabric's entries are held to otaInProgressPerFabric, the oldest
// leaving first.
func TestOTAProvider_InProgressBoundedPerFabric(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query %+v", r)
	}
	for i := range 500 {
		token := make([]byte, 8)
		binary.BigEndian.PutUint64(token, uint64(i)+1)
		if a := f.apply(t, token, 2); a.Action != provdef.ApplyUpdateActionProceed {
			t.Fatalf("apply %d: %+v", i, a)
		}
	}
	f.p.mu.Lock()
	n := len(f.p.inProgress)
	f.p.mu.Unlock()
	if n > otaInProgressPerFabric {
		t.Errorf("%d in-progress entries after 500 unknown tokens, want at most %d", n, otaInProgressPerFabric)
	}
}

// TestOTAProvider_GroupMessageIsNotCASE (decision 158 brief: the CASE check
// is internal/caseauth's): a group message carries a fabric and is not
// PASE, and is still refused.
func TestOTAProvider_GroupMessageIsNotCASE(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	ctx := im.WithGroupSubject(otaCtx(7), im.GroupSubject{GroupID: 1})
	_, err := f.p.MatterInvoke(ctx, provdef.CmdQueryImage, bdxQuery())
	var se interface{ MatterStatusCode() im.StatusCode }
	if !errors.As(err, &se) || se.MatterStatusCode() != im.StatusUnsupportedAccess {
		t.Fatalf("group QueryImage: %v", err)
	}
}
