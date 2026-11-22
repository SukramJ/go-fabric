// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups_test

import (
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/groups"
)

// TestDeprecatedNoKeyErrorStillIsErrNoKey: the deprecated NoKeyError, which
// Manager.Decode no longer returns, keeps what a host's code written
// against it relies on until its removal — errors.Is matches ErrNoKey, and
// the message names the fabric and group of an authenticated one.
func TestDeprecatedNoKeyErrorStillIsErrNoKey(t *testing.T) {
	t.Parallel()
	var err error = &groups.NoKeyError{} //nolint:staticcheck // the deprecated type is what this test holds
	if !errors.Is(err, groups.ErrNoKey) {
		t.Fatal("errors.Is(&NoKeyError{}, ErrNoKey) = false")
	}
	if got := err.Error(); got != groups.ErrNoKey.Error() {
		t.Errorf("Error() = %q, want ErrNoKey's text", got)
	}
	err = &groups.NoKeyError{Authenticated: true, FabricIndex: 2, GroupID: 7} //nolint:staticcheck // as above
	if got := err.Error(); !strings.Contains(got, "fabric 2") || !strings.Contains(got, "group 7") {
		t.Errorf("Error() = %q, want the fabric and the group named", got)
	}
}
