// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/controller/client"
)

// InventoryEntry is one commissioned node of the fabric with the
// onboarding code kept for it: what a host shows or prints so that the
// code survives the paper it came on. The strings are the module's part;
// drawing the QR code is the host's.
//
// QRCode and ManualCode are secrets: with one of them and physical access
// to the device (a factory reset) anyone can commission it. They are
// [client.SecretCode] values, redacted wherever an entry is formatted,
// logged or serialised; Reveal reads the text, and what a host does with
// it — an export, an API response — needs the protection of the
// controller's database.
type InventoryEntry struct {
	// Node is the node.
	Node *client.Node
	// NodeID is its node id on the fabric.
	NodeID uint64
	// Label is the node's NodeLabel, the name its user gave it; "" when
	// unset or not read yet.
	Label string
	// VendorName, ProductName and SerialNumber are the node's Basic
	// Information; "" when the image does not hold the attribute.
	VendorName, ProductName, SerialNumber string
	// VendorID and ProductID are the node's ids, 0 when unknown.
	VendorID, ProductID uint16
	// HasCode reports a stored onboarding code; the fields below are
	// zero without one.
	HasCode bool
	// QRCode is the QR code's text ("MT:…"); zero when the stored code is
	// a manual pairing code, from which no QR code is made up.
	QRCode client.SecretCode
	// ManualCode is the manual pairing code, 11 or 21 digits.
	ManualCode client.SecretCode
	// Source tells whether the device accepted the code at pairing or it
	// was attached afterwards and is unproven.
	Source client.OnboardingSource
	// Checks are the identity checks an attached code passed.
	Checks client.OnboardingChecks
	// StoredAt is when the code was stored.
	StoredAt time.Time
}

// String keeps the codes out of a formatted entry.
func (e InventoryEntry) String() string {
	code := "no code"
	if e.HasCode {
		code = "code redacted"
	}
	return "InventoryEntry(node " + strconv.FormatUint(e.NodeID, 10) + ", " + code + ")"
}

// GoString keeps the codes out of a %#v.
func (e InventoryEntry) GoString() string { return e.String() }

// LogValue keeps the codes out of a log record.
func (e InventoryEntry) LogValue() slog.Value { return slog.StringValue(e.String()) }

// Inventory lists every commissioned node with its onboarding code, in
// the order of [Controller.Nodes]. A node has a code when [Controller.Pair]
// kept it ([Config].KeepOnboardingCodes), or one was attached to it
// (client.Node.SetOnboardingCode); a code cannot be read from a
// commissioned device, which holds a verifier and not the passcode. A
// stored code that no longer decodes is listed as none; a node
// decommissioned while the list is built is left out.
func (c *Controller) Inventory() []InventoryEntry {
	nodes := c.reg.Commissioned()
	out := make([]InventoryEntry, 0, len(nodes))
	for _, n := range nodes {
		addr, ok := n.PeerAddress()
		if !ok {
			continue
		}
		e := InventoryEntry{Node: n, NodeID: addr.NodeID}
		rec := n.Record()
		if rec.VendorID != nil {
			e.VendorID = *rec.VendorID
		}
		if rec.ProductID != nil {
			e.ProductID = *rec.ProductID
		}
		if root := n.Endpoint(0); root != nil {
			bi := root.Cluster(basicinformation.ClusterID)
			e.Label = stringOf(bi, basicinformation.AttrNodeLabel)
			e.VendorName = stringOf(bi, basicinformation.AttrVendorName)
			e.ProductName = stringOf(bi, basicinformation.AttrProductName)
			e.SerialNumber = stringOf(bi, basicinformation.AttrSerialNumber)
			if v, ok := uint16Of(bi, basicinformation.AttrVendorId); ok {
				e.VendorID = v
			}
			if v, ok := uint16Of(bi, basicinformation.AttrProductId); ok {
				e.ProductID = v
			}
		}
		if code, ok := n.OnboardingCode(); ok {
			if qr, manual, err := code.Codes(); err == nil {
				e.HasCode, e.QRCode, e.ManualCode = true, qr, manual
				e.Source, e.Checks, e.StoredAt = code.Source, code.Checks, code.StoredAt
			}
		}
		out = append(out, e)
	}
	return out
}

// stringOf is a string attribute of the cluster, "" when absent or not a
// string.
func stringOf(c *client.Cluster, attr uint32) string {
	v, ok := c.Value(attr)
	if !ok {
		return ""
	}
	s, err := spec.DecodeString(v)
	if err != nil {
		return ""
	}
	return s
}

// uint16Of is a 16-bit unsigned attribute of the cluster.
func uint16Of(c *client.Cluster, attr uint32) (uint16, bool) {
	v, ok := c.Value(attr)
	if !ok {
		return 0, false
	}
	u, err := spec.DecodeUint[uint16](16)(v)
	return u, err == nil
}
