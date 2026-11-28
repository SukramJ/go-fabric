// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/controller/attestation"
)

// panickingStore is a host PAAStore whose root lookup panics.
type panickingStore struct {
	*attestation.StaticStore
}

func (panickingStore) PAA([]byte) (attestation.PAA, bool) {
	panic("host store broke")
}

// TestAttestationStorePanicIsAFinding: a panic in the host's PAAStore,
// called on the validation's own goroutine, does not end the process; it
// becomes one error finding of check ValidationError that the
// OnAttestationFailure policy judges, naming the panic value by type only
// (review II, P10-err-6202).
func TestAttestationStorePanicIsAFinding(t *testing.T) {
	t.Parallel()
	d := newAttestingDevice(t)
	var seen []attestation.Finding
	f := attestingFlow(t, d, attestation.Policy{Decide: func(fs []attestation.Finding) (bool, string, error) {
		seen = fs
		return false, "", nil
	}})
	f.c.ctx.Attestation = panickingStore{attestation.TestStore()}
	if _, err := f.deviceAttestation(context.Background()); err == nil {
		t.Fatal("deviceAttestation succeeded after the store panicked")
	}
	if len(seen) != 1 || seen[0].Check != attestation.ValidationError || seen[0].Level != attestation.LevelError {
		t.Fatalf("findings %+v, want one ValidationError", seen)
	}
	if msg := seen[0].Message; !strings.Contains(msg, "panicked") || !strings.Contains(msg, "string") || strings.Contains(msg, "host store broke") {
		t.Fatalf("finding message %q, want the panic named by type only", msg)
	}
}
