// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec

import (
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/tlv"
)

// The registry maps a cluster id to its generated definition. A generated
// package registers its definition when it is linked in (its init), so the
// bridge can decode the request payloads of every cluster a host serves
// through a generated definition without importing the cluster packages.
var registry sync.Map // uint32 → *Cluster

// Register adds definitions to the registry; a later registration of the
// same cluster id replaces the earlier one.
func Register(defs ...*Cluster) {
	for _, d := range defs {
		registry.Store(d.ID, d)
	}
}

// Lookup returns the registered definition of clusterID, or nil.
func Lookup(clusterID uint32) *Cluster {
	if v, ok := registry.Load(clusterID); ok {
		d, _ := v.(*Cluster)
		return d
	}
	return nil
}

// DecodeRequest decodes the fields of a request command of a registered
// cluster: open is the fields container's opening element, already read
// from dec, and the container is read through its EndContainer. It reports
// false — and reads nothing — for a cluster or command no registered
// definition decodes, leaving the payload to the caller.
//
// A payload the request's schema rejects comes back as a [*FieldError]
// with the status matter.js answers it with; a malformed TLV stream as a
// plain error.
func DecodeRequest(clusterID, cmdID uint32, dec *tlv.Decoder, open tlv.Element) (fields any, ok bool, err error) {
	def := Lookup(clusterID)
	if def == nil {
		return nil, false, nil
	}
	cmd := def.Command(cmdID, Request)
	if cmd == nil || cmd.Decode == nil {
		return nil, false, nil
	}
	n, err := ReadContainer(dec, open)
	if err != nil {
		return nil, true, fmt.Errorf("%s.%s: %w", def.Name, cmd.Name, err)
	}
	fields, err = cmd.Decode(n)
	return fields, true, err
}
