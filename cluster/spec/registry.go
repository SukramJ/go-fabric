// SPDX-License-Identifier: Apache-2.0
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

// Register adds definitions to the registry. Registering the definition a
// cluster id already holds is a no-op; a different definition under an id
// already registered is a programming error and panics, as
// database/sql.Register does for a driver name — a definition replaced
// silently would change the request decoding of every server of that
// cluster in the process (review II, R2-P6-api-1311). Register a vendor
// cluster under its own (vendor-prefixed) id.
//
// A hand-written definition (a vendor cluster) is supported beside the
// generated ones. Its request commands' Decode turns a payload into its
// request type, holding the types and constraints its decoder is written
// to hold, as a generated decoder holds the ones the generator renders;
// the endpoint dispatcher then holds the decoded request to the definition
// before the server runs ([AdmitRequest]):
// a command whose [RequestCheck] states a Refusal answers a request that
// does not fit with it, every other command refuses a value an enum does
// not define with CONSTRAINT_ERROR and logs any other failure and runs
// ([AdmitRequest]).
func Register(defs ...*Cluster) {
	for _, d := range defs {
		if held, loaded := registry.LoadOrStore(d.ID, d); loaded && held != d {
			name := ""
			if h, ok := held.(*Cluster); ok {
				name = h.Name
			}
			panic(fmt.Sprintf("spec: cluster 0x%04X registered twice (%s, then %s)", d.ID, name, d.Name))
		}
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
	fields, err = cmd.DecodeRequest(n)
	return fields, true, err
}
