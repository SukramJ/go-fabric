// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package contract_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
)

// TestStatusErrorText: the text names the status, the cluster status when
// set and the message, and a wrapped StatusError is found by errors.As by
// value.
func TestStatusErrorText(t *testing.T) {
	t.Parallel()
	cs := uint8(0x42)
	for _, c := range []struct {
		err  contract.StatusError
		want string
	}{
		{contract.StatusError{Status: contract.StatusTimeout}, "matter status 0x94"},
		{contract.StatusError{Status: 0x01, ClusterStatus: &cs}, "matter status 0x01, cluster status 0x42"},
		{contract.StatusError{Status: 0x87, Message: "out of range"}, "matter status 0x87: out of range"},
	} {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() %q, want %q", got, c.want)
		}
	}
	var v contract.StatusError
	if !errors.As(fmt.Errorf("x: %w", contract.StatusError{Status: 0x9C}), &v) || v.Status != 0x9C {
		t.Fatal("a wrapped value is not found")
	}
}
