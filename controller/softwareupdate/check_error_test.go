// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/controller/client"
)

// TestProductCheckErrorNamesTheProductAndItsNodes (go-fabric#555): a
// failed product's error names the product, the version and the nodes it
// was checked for, in the targets' order, and reaches its cause.
func TestProductCheckErrorNamesTheProductAndItsNodes(t *testing.T) {
	t.Parallel()
	a, b := Peer{FabricIndex: 1, NodeID: 0x11}, Peer{FabricIndex: 1, NodeID: 0x22}
	err := productCheckFailed(0xFFF1, 0x8001, 3, []prepared{{peer: a}, {peer: b}}, fmt.Errorf("%w: no checksum", ErrOTA))
	var pe *ProductCheckError
	if !errors.As(err, &pe) || !errors.Is(err, ErrOTA) {
		t.Fatalf("%v: want a *ProductCheckError reaching ErrOTA", err)
	}
	if pe.VendorID != 0xFFF1 || pe.ProductID != 0x8001 || pe.SoftwareVersion != 3 || !slices.Equal(pe.Peers, []Peer{a, b}) {
		t.Fatalf("%+v, want product fff1-8001 at version 3 for both nodes", pe)
	}
	if got, want := err.Error(), "softwareupdate: check of product fff1-8001 at version 3 for 2 node(s): softwareupdate: OTA update failed: no checksum"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestProductCheckErrorsWalksTheJoin (go-fabric#555): ProductCheckErrors
// returns every product failure a joined or wrapped error holds, in order,
// and none for nil or an error without one.
func TestProductCheckErrorsWalksTheJoin(t *testing.T) {
	t.Parallel()
	first := &ProductCheckError{VendorID: 1, ProductID: 1, Err: ErrOTA}
	second := &ProductCheckError{VendorID: 2, ProductID: 2, Err: &ProductCheckError{VendorID: 9}}
	err := fmt.Errorf("check: %w", errors.Join(first, errors.New("other"), errors.Join(second)))
	if got := ProductCheckErrors(err); !slices.Equal(got, []*ProductCheckError{first, second}) {
		t.Fatalf("ProductCheckErrors = %v, want the two products in order (not the nested cause)", got)
	}
	if got := ProductCheckErrors(nil); got != nil {
		t.Fatalf("ProductCheckErrors(nil) = %v", got)
	}
	if got := ProductCheckErrors(errors.New("plain")); got != nil {
		t.Fatalf("ProductCheckErrors(plain) = %v", got)
	}
}

// TestQueryUpdatesNamesTheFailedProducts (go-fabric#555): the error of
// QueryUpdates and CheckForUpdates carries each failed product with its
// nodes beside the updates found, so a caller attributes the failures
// without checking node by node again.
func TestQueryUpdatesNamesTheFailedProducts(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	failed := productCheckFailed(0xFFF1, 0x8000, 1, []prepared{{peer: testPeer}}, ErrOTA)
	r.m.query = func(context.Context, *client.Node, bool) ([]PeerUpdate, bool, error) {
		return []PeerUpdate{{Peer: Peer{FabricIndex: 1, NodeID: 0x33}}}, true, errors.Join(failed)
	}
	got, err := r.m.QueryUpdates(context.Background(), nil, true)
	pes := ProductCheckErrors(err)
	if len(got) != 1 || len(pes) != 1 || !slices.Equal(pes[0].Peers, []Peer{testPeer}) {
		t.Fatalf("QueryUpdates = %v, %v; want the update found and the failed product with its node", got, err)
	}
	if pes := ProductCheckErrors(r.m.CheckForUpdates(context.Background())); len(pes) != 1 || pes[0].ProductID != 0x8000 {
		t.Fatalf("CheckForUpdates' product failures %v, want fff1-8000", pes)
	}
}
