// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import "fmt"

// ProductCheckError is one product's failed check in the error
// [Manager.QueryUpdates] and [Manager.CheckForUpdates] return, which joins
// one per failed product (errors.Join): the product, the software version
// its nodes run, the nodes it was checked for, and the cause, which
// errors.Is and errors.As reach through it. A caller attributes a failure
// to its nodes with [ProductCheckErrors] instead of checking node by node
// again (go-fabric#555).
type ProductCheckError struct {
	VendorID, ProductID uint16
	// SoftwareVersion is the version the nodes run, which the check
	// looked for a newer one than.
	SoftwareVersion uint32
	// Peers are the nodes of the product at that version the check was
	// for, in the registry's order.
	Peers []Peer
	Err   error
}

func (e *ProductCheckError) Error() string {
	return fmt.Sprintf("softwareupdate: check of product %04x-%04x at version %d for %d node(s): %v",
		e.VendorID, e.ProductID, e.SoftwareVersion, len(e.Peers), e.Err)
}

func (e *ProductCheckError) Unwrap() error { return e.Err }

// ProductCheckErrors are the [ProductCheckError]s err holds, in the order
// the products were checked; none for nil or an error without one. It
// walks errors.Join and wrapping alike, and does not descend into a
// ProductCheckError's cause.
func ProductCheckErrors(err error) []*ProductCheckError {
	var out []*ProductCheckError
	var walk func(error)
	walk = func(err error) {
		if err == nil {
			return
		}
		if pe, ok := err.(*ProductCheckError); ok { //nolint:errorlint // the walk below is the unwrapping
			out = append(out, pe)
			return
		}
		switch u := err.(type) { //nolint:errorlint // the walk below is the unwrapping
		case interface{ Unwrap() []error }:
			for _, e := range u.Unwrap() {
				walk(e)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)
	return out
}

// productCheckFailed is the [ProductCheckError] of a product whose check
// for targets failed with err.
func productCheckFailed(vid, pid uint16, version uint32, targets []prepared, err error) error {
	pe := &ProductCheckError{VendorID: vid, ProductID: pid, SoftwareVersion: version, Err: err}
	for _, t := range targets {
		pe.Peers = append(pe.Peers, t.peer)
	}
	return pe
}
