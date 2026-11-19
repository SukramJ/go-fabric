// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"errors"
	"fmt"
	"slices"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	admincommdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	groupcastdef "github.com/SukramJ/go-fabric/cluster/spec/groupcast"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	opcredsdef "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	opdef "github.com/SukramJ/go-fabric/cluster/spec/operationalstate"
	scenesdef "github.com/SukramJ/go-fabric/cluster/spec/scenesmanagement"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// commandFieldsReader is the [im.CommandFieldsReader] the bridge plugs
// into [im.UnmarshalInvokeRequestTLV]. A request of a cluster with a
// registered generated definition decodes through it
// ([decodeByDefinition]) into the generated request struct
// (<cluster>def.<Command>Request) every self-held server takes as its
// primary form — one decoder, from one source (ADR 0026 principle 3).
// The commissioning, group and scene requests keep their hand-written
// readers, whose servers are written against their cluster/core request
// structs (notes/parity/by_design.md BD-Matter-Commissioning-HandDecoders,
// BD-Matter-AdminCommissioning-OpenWindowParams); each is first held to
// its definition's request check ([heldByDefinition]), as every other
// request is. A command no definition decodes is salvaged as a tag map.
//
// Decode failures wrap with the cluster / command identifier so operator
// triage stays specific.
func commandFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
	if def, read := handReader(path); read != nil {
		return heldByDefinition(def, path, dec, el, read)
	}
	// Every other cluster with a generated definition (cluster/spec/...)
	// decodes its request payloads through it, with the statuses
	// matter.js's request schema answers a rejected payload with.
	if fields, ok, err := decodeByDefinition(path, dec, el); ok {
		return fields, err
	}
	// Unknown command-path: salvage as a tag-keyed map[uint8]any so the
	// cluster server gets the field payload regardless of whether the
	// bridge has a typed decoder yet. Without this every Apple-driven
	// invoke on a non-commissioning cluster reaches MatterInvoke with
	// fields=nil — silently breaking dimming (LevelControl.MoveToLevel),
	// blinds (WindowCovering.GoToLiftPercentage), color (ColorControl.*),
	// thermostat setpoint, and lock-with-PIN commands.
	return decodeGenericTagMap(dec)
}

// handRead is a hand-written request reader: ok is false for a command it
// does not decode.
type handRead func(path im.ConcreteCommandPath, dec *tlv.Decoder) (fields any, ok bool, err error)

// handReader is the hand-written reader of path's cluster and the
// definition its requests are held to; nil for a cluster without one.
func handReader(path im.ConcreteCommandPath) (*spec.Cluster, handRead) {
	switch path.Cluster {
	case gencommdef.ClusterID:
		return gencommdef.Definition, generalCommissioningFieldsReader
	case opcredsdef.ClusterID:
		return opcredsdef.Definition, operationalCredentialsFieldsReader
	case administratorCommissioningClusterID:
		return admincommdef.Definition, func(path im.ConcreteCommandPath, dec *tlv.Decoder) (any, bool, error) {
			if path.Command != 0x00 { // OpenCommissioningWindow
				return nil, false, nil
			}
			fields, err := decodeOpenCommissioningWindowRequest(dec)
			return fields, true, err
		}
	case mattercore.GroupsClusterID:
		return groupsdef.Definition, groupCommandFieldsReader
	case gkmdef.ClusterID:
		return gkmdef.Definition, groupCommandFieldsReader
	case mattercore.GroupcastClusterID:
		return groupcastdef.Definition, groupcastFieldsReader
	case mattercore.ScenesManagementClusterID:
		return scenesdef.Definition, scenesFieldsReader
	}
	return nil, nil
}

// heldByDefinition reads a request the bridge decodes by hand into a node,
// holds it to its definition's request check — the request schema matter.js
// validates every request against before the server runs, with the
// server's relaxations the definition carries
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:474-487 at
// db021da0; [spec.Command.DecodeRequest]) — and hands the same fields to
// the hand-written reader. A command the reader does not decode goes on to
// the definition's decoder, else is salvaged as a tag map, as before. The
// fields container is consumed, so every refusal says so
// ([im.FieldsContainerConsumed]).
func heldByDefinition(def *spec.Cluster, path im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element, read handRead) (any, error) {
	n, err := spec.ReadContainer(dec, el)
	if err != nil {
		return nil, fieldsReadError(fmt.Sprintf("%s command 0x%02X", def.Name, path.Command), err)
	}
	cmd := def.Command(path.Command, spec.Request)
	if cmd != nil && cmd.Decode == nil {
		// A command the definition knows but cannot check: the hand
		// reader's own checks are gone (R2-P6-test-0811), so it is not
		// read unchecked — refused, fail-closed. No command of the
		// hand-read clusters is such today (40 of 40 have a decoder).
		return nil, &spec.FieldError{Status: im.StatusInvalidCommand, Msg: fmt.Sprintf("%s.%s: no request decoder to hold the request to", def.Name, cmd.Name)}
	}
	if cmd != nil {
		if _, err := cmd.DecodeRequest(n); err != nil {
			return nil, err
		}
		// The hand reader reads the checked request: the top-level fields
		// the definition defines, each the element the check judged. A
		// member the definition does not know — skipped whole by its
		// decoder, as matter.js skips an unknown field
		// (packages/types/src/tlv/TlvObject.ts:215-217 at b25ed960) — never
		// reaches a reader that would descend into it and read its inner
		// tags as the command's own (R2-P6-wire-0440).
		n = definedFields(n, cmd)
		// Only a request its definition checked reaches a hand reader: a
		// command the definition does not know goes on to the salvage
		// below, and the dispatcher answers its path.
		sub, _, err := reopen(n)
		if err != nil {
			return nil, err
		}
		fields, ok, err := read(path, sub)
		if ok {
			return fields, consumed(err)
		}
		if err != nil {
			return nil, consumed(err)
		}
	}
	sub, open, err := reopen(n)
	if err != nil {
		return nil, err
	}
	if fields, ok, err := decodeByDefinition(path, sub, open); ok {
		return fields, consumed(err)
	}
	if sub, _, err = reopen(n); err != nil {
		return nil, err
	}
	fields, err := decodeGenericTagMap(sub)
	return fields, consumed(err)
}

// definedFields returns the request tree n with only the top-level members
// whose context tag is one of cmd's fields; anonymous and profile-tagged
// members, and context tags the command does not define, are left out.
func definedFields(n spec.Node, cmd *spec.Command) spec.Node {
	out := spec.Node{El: n.El, Children: make([]spec.Node, 0, len(n.Children))}
	for _, c := range n.Children {
		if c.El.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if slices.ContainsFunc(cmd.Fields, func(f spec.Field) bool { return f.ID == c.El.Tag.Number }) {
			out.Children = append(out.Children, c)
		}
	}
	return out
}

// fieldsReadError is the refusal of command fields [spec.ReadContainer]
// could not read as a tree. An array with a tagged element is a payload
// the command's schema does not decode — INVALID_COMMAND for this command
// alone, as matter.js answers a payload its schema refuses
// (packages/types/src/tlv/TlvArray.ts:77-78 throws,
// packages/protocol/src/action/server/CommandInvokeResponse.ts:549-563 at
// db021da0 turns it into InvalidCommandError); the container has been read
// through, which the [spec.FieldError] says. Any other failure is a
// malformed request and passes without a status.
func fieldsReadError(what string, err error) error {
	if errors.Is(err, spec.ErrArrayElementTag) {
		return &spec.FieldError{Status: im.StatusInvalidCommand, Msg: fmt.Sprintf("%s: %v", what, err)}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// reopen is a decoder over n's encoding, positioned inside its container
// as a [im.CommandFieldsReader] receives it, and the container's opening
// element.
func reopen(n spec.Node) (*tlv.Decoder, tlv.Element, error) {
	enc := tlv.NewEncoder()
	n.EncodeTLV(enc, n.El.Tag)
	raw, err := enc.Bytes()
	if err != nil {
		return nil, tlv.Element{}, fmt.Errorf("fields: %w", err)
	}
	d := tlv.NewDecoder(raw)
	open, err := d.Next()
	if err != nil {
		return nil, tlv.Element{}, fmt.Errorf("fields: %w", err)
	}
	return d, open, nil
}

// consumedReject is a reader's typed reject after [heldByDefinition] read
// the fields container through its end.
type consumedReject struct{ error }

// MatterStatusCode implements [im.StatusCodeError].
func (e consumedReject) MatterStatusCode() im.StatusCode {
	if sce, ok := errors.AsType[im.StatusCodeError](e.error); ok {
		return sce.MatterStatusCode()
	}
	return im.StatusSuccess
}

// FieldsContainerConsumed implements [im.FieldsContainerConsumed].
func (consumedReject) FieldsContainerConsumed() bool { return true }

// Unwrap returns the reader's error.
func (e consumedReject) Unwrap() error { return e.error }

// consumed marks a typed reject as one that consumed the container; nil
// and an error without a status (a malformed request) pass as they are.
func consumed(err error) error {
	if _, ok := errors.AsType[im.StatusCodeError](err); !ok {
		return err
	}
	return consumedReject{err}
}

// generalCommissioningFieldsReader is the hand-written reader of the
// GeneralCommissioning requests.
func generalCommissioningFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder) (fields any, ok bool, err error) {
	switch path.Command {
	case 0x00:
		fields, err = decodeArmFailSafeRequest(dec)
	case 0x02:
		fields, err = decodeSetRegulatoryConfigRequest(dec)
	case 0x04:
		// CommissioningComplete carries no field: path-only Invoke.
		err = drainContainer(dec)
	default:
		return nil, false, nil
	}
	return fields, true, err
}

// operationalCredentialsFieldsReader is the hand-written reader of the
// OperationalCredentials requests.
func operationalCredentialsFieldsReader(path im.ConcreteCommandPath, dec *tlv.Decoder) (f any, ok bool, err error) { //nolint:gocyclo // a dispatch table: one case per command
	switch path.Command {
	case 0x00:
		f, err = decodeAttestationRequest(dec)
	case 0x02:
		f, err = decodeCertificateChainRequest(dec)
	case 0x04:
		f, err = decodeCSRRequest(dec)
	case 0x06:
		f, err = decodeAddNOCRequest(dec)
	case 0x07:
		f, err = decodeUpdateNOCRequest(dec)
	case 0x09:
		f, err = decodeUpdateFabricLabelRequest(dec)
	case 0x0A:
		f, err = decodeRemoveFabricRequest(dec)
	case 0x0B:
		f, err = decodeAddTrustedRootCertificateRequest(dec)
	case 0x0C:
		f, err = decodeSetVidVerificationStatementRequest(dec)
	case 0x0D:
		f, err = decodeSignVidVerificationRequest(dec)
	default:
		return nil, false, nil
	}
	return f, true, err
}

// decodeGenericTagMap drains the current container into a
// map[uint8]any keyed by context-tag number, preserving the field
// payload for cluster servers that don't yet have a typed decoder.
// Skips nested containers (drains them) — typed decoders cover the
// nested-struct cases. Returns nil-map when the container is empty.
func decodeGenericTagMap(dec *tlv.Decoder) (map[uint8]any, error) {
	var out map[uint8]any
	for {
		el, err := dec.Next()
		if err != nil {
			return nil, fmt.Errorf("generic fields: %w", err)
		}
		if el.IsEndContainer {
			return out, nil
		}
		if el.IsContainer {
			if err := drainContainer(dec); err != nil {
				return nil, fmt.Errorf("drain nested container: %w", err)
			}
			continue
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if out == nil {
			out = make(map[uint8]any, 4)
		}
		tag := uint8(el.Tag.Number & 0xFF)
		switch {
		case el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8:
			out[tag] = el.Uint
		case el.Type >= tlv.TypeSignedInt1 && el.Type <= tlv.TypeSignedInt8:
			out[tag] = el.Int
		case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
			out[tag] = el.Bool
		case el.Type == tlv.TypeFloat4 || el.Type == tlv.TypeFloat8:
			out[tag] = el.Float
		case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
			out[tag] = el.String
		case el.IsNull:
			out[tag] = nil
		}
		if el.Type >= tlv.TypeOctetStr1 && el.Type <= tlv.TypeOctetStr8 {
			out[tag] = append([]byte(nil), el.Octets...)
		}
	}
}

// decodeArmFailSafeRequest reads the GeneralCommissioning ArmFailSafe
// command fields per Matter §11.10.6.2. Tags: [0] uint16
// ExpiryLengthSeconds, [1] uint64 Breadcrumb.
func decodeArmFailSafeRequest(dec *tlv.Decoder) (mattercore.ArmFailSafeRequest, error) {
	var req mattercore.ArmFailSafeRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("ArmFailSafe: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			req.ExpiryLengthSeconds = uint16(el.Uint) //nolint:gosec // the definition held the width
		case 1:
			req.Breadcrumb = el.Uint
		}
	}
}

// decodeSetRegulatoryConfigRequest reads the SetRegulatoryConfig
// command fields per Matter §11.10.6.4. Tags: [0] enum8
// NewRegulatoryConfig, [1] string CountryCode, [2] uint64 Breadcrumb.
func decodeSetRegulatoryConfigRequest(dec *tlv.Decoder) (mattercore.SetRegulatoryConfigRequest, error) {
	var req mattercore.SetRegulatoryConfigRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("SetRegulatoryConfig: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			req.NewRegulatoryConfig = uint8(el.Uint) //nolint:gosec // the definition held the width
		case 1:
			// CountryCode is a `TlvUTF8String` (char-string[2] per
			// §11.10.7.4). TLV decoder lands UTF8 in `el.String`,
			// not `el.Octets` — but the old `el.Octets` read had
			// silently dropped every value to "" for an unknown
			// duration. Run 24 with the el.String
			// fix in place showed Apple sending RemoveFabric ~80 s
			// after Subscribe-Initial — pre-fix Run 23 reached
			// `Completed rebuilding HAP services`. The CountryCode
			// fix is therefore reverted until the SetRegulatoryConfig
			// handler is verified to accept non-empty values without
			// regressing the pair flow. Reactivate after handler audit.
			req.CountryCode = string(el.Octets)
			_ = el.String // explicit unused — see comment above
		case 2:
			req.Breadcrumb = el.Uint
		}
	}
}

// decodeAttestationRequest reads OperationalCredentials AttestationRequest
// (Matter §11.18.7.1). Tag [0] = AttestationNonce (32-byte octets).
func decodeAttestationRequest(dec *tlv.Decoder) (mattercore.AttestationRequest, error) {
	var req mattercore.AttestationRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("AttestationRequest: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if uint8(el.Tag.Number&0xFF) == 0 {
			req.AttestationNonce = append([]byte(nil), el.Octets...)
		}
	}
}

// decodeCertificateChainRequest reads OperationalCredentials
// CertificateChainRequest (Matter §11.18.7.3). Tag [0] = CertificateType (uint8).
func decodeCertificateChainRequest(dec *tlv.Decoder) (mattercore.CertificateChainRequest, error) {
	var req mattercore.CertificateChainRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("CertificateChainRequest: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if uint8(el.Tag.Number&0xFF) == 0 {
			req.CertificateType = uint8(el.Uint) //nolint:gosec // the definition held the width
		}
	}
}

// decodeCSRRequest reads OperationalCredentials CSRRequest (§11.18.7.5).
// Tags: [0] CSRNonce (32 bytes), [1] IsForUpdateNOC (bool, optional).
func decodeCSRRequest(dec *tlv.Decoder) (mattercore.CSRRequest, error) {
	var req mattercore.CSRRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("CSRRequest: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			req.CSRNonce = append([]byte(nil), el.Octets...)
		case 1:
			req.IsForUpdateNOC = el.Bool
		}
	}
}

// decodeAddNOCRequest reads OperationalCredentials AddNOC (§11.18.7.7).
// Tags: [0] NOCValue, [1] ICACValue, [2] IPKValue (16B),
// [3] CaseAdminSubject (uint64), [4] AdminVendorID (uint16).
func decodeAddNOCRequest(dec *tlv.Decoder) (mattercore.AddNOCRequest, error) {
	var req mattercore.AddNOCRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("AddNOC: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			req.NOCValue = append([]byte(nil), el.Octets...)
		case 1:
			req.ICACValue = append([]byte(nil), el.Octets...)
		case 2:
			req.IPKValue = append([]byte(nil), el.Octets...)
		case 3:
			req.CaseAdminSubject = el.Uint
		case 4:
			req.AdminVendorID = uint16(el.Uint) //nolint:gosec // the definition held the width
		}
	}
}

// decodeUpdateNOCRequest reads OperationalCredentials UpdateNOC (§11.18.7.8).
// Tags: [0] NOCValue, [1] ICACValue.
func decodeUpdateNOCRequest(dec *tlv.Decoder) (mattercore.UpdateNOCRequest, error) {
	var req mattercore.UpdateNOCRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("UpdateNOC: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		switch uint8(el.Tag.Number & 0xFF) {
		case 0:
			req.NOCValue = append([]byte(nil), el.Octets...)
		case 1:
			req.ICACValue = append([]byte(nil), el.Octets...)
		}
	}
}

// decodeUpdateFabricLabelRequest reads UpdateFabricLabel (§11.18.7.10).
// Tag [0] = Label (UTF8 string).
//
// The field is `TlvUTF8String` per the spec — chip's TLV reader lands
// the decoded value in `el.String`, not `el.Octets` (which is reserved
// for `octstr` types per `tlv/decode.go:182,193`).
// The old code read `el.Octets` and got an empty string back for
// every Apple-Home pair, so we acked `UpdateFabricLabel("")` with
// `NOCStatus=OK` while Apple's HomeKitDaemon had actually sent
// `"Mein Zuhause"`. Apple's subsequent FabricInformation read found
// the label empty and surfaced `HAPErrorDomain Code=1 "Failed to
// update fabric label for accessory"` — the bridge then went to
// `MTRDeviceStateUnreachable` 4 s later.
func decodeUpdateFabricLabelRequest(dec *tlv.Decoder) (mattercore.UpdateFabricLabelRequest, error) {
	var req mattercore.UpdateFabricLabelRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("UpdateFabricLabel: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if uint8(el.Tag.Number&0xFF) == 0 {
			req.Label = el.String
		}
	}
}

// decodeRemoveFabricRequest reads RemoveFabric (§11.18.7.11). Tag [0]
// = FabricIndex (uint8).
func decodeRemoveFabricRequest(dec *tlv.Decoder) (mattercore.RemoveFabricRequest, error) {
	var req mattercore.RemoveFabricRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("RemoveFabric: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if uint8(el.Tag.Number&0xFF) == 0 {
			req.FabricIndex = uint8(el.Uint) //nolint:gosec // the definition held the width
		}
	}
}

// decodeAddTrustedRootCertificateRequest reads AddTrustedRootCertificate
// (§11.18.7.12). Tag [0] = RootCACertificate (octets).
func decodeAddTrustedRootCertificateRequest(dec *tlv.Decoder) (mattercore.AddTrustedRootCertificateRequest, error) {
	var req mattercore.AddTrustedRootCertificateRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("AddTrustedRootCertificate: %w", err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			continue
		}
		if uint8(el.Tag.Number&0xFF) == 0 {
			req.RootCACertificate = append([]byte(nil), el.Octets...)
		}
	}
}

// decodeSetVidVerificationStatementRequest reads SetVIDVerificationStatement
// (operational-credentials.element.ts, command 0x0C): [0] VendorID
// (vendor-id, optional), [1] VIDVerificationStatement (octstr max 85,
// optional), [2] VVSC (octstr max 400, optional), recording which were
// present — an empty statement or VVSC that is present clears it.
func decodeSetVidVerificationStatementRequest(dec *tlv.Decoder) (mattercore.SetVidVerificationStatementRequest, error) {
	const cmd = "SetVIDVerificationStatement"
	var req mattercore.SetVidVerificationStatementRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipValue(dec, el); err != nil {
				return req, err
			}
			continue
		}
		switch el.Tag.Number {
		case 0:
			req.VendorID = uint16(el.Uint) //nolint:gosec // the definition held the width
			req.HasVendorID = true
		case 1:
			req.VidVerificationStatement = append([]byte{}, el.Octets...)
			req.HasVidVerificationStatement = true
		case 2:
			req.Vvsc = append([]byte{}, el.Octets...)
			req.HasVvsc = true
		default:
			if err := skipValue(dec, el); err != nil {
				return req, err
			}
		}
	}
}

// decodeSignVidVerificationRequest reads SignVIDVerificationRequest
// (command 0x0D): [0] FabricIndex (mandatory), [1] ClientChallenge (octstr
// of exactly 32 bytes, mandatory).
func decodeSignVidVerificationRequest(dec *tlv.Decoder) (mattercore.SignVidVerificationRequest, error) {
	const cmd = "SignVIDVerificationRequest"
	var req mattercore.SignVidVerificationRequest
	for {
		el, err := dec.Next()
		if err != nil {
			return req, fmt.Errorf("%s: %w", cmd, err)
		}
		if el.IsEndContainer {
			return req, nil
		}
		if el.Tag.Kind != tlv.TagKindContext {
			if err := skipValue(dec, el); err != nil {
				return req, err
			}
			continue
		}
		switch el.Tag.Number {
		case 0:
			req.FabricIndex = uint8(el.Uint) //nolint:gosec // the definition held the width
		case 1:
			req.ClientChallenge = append([]byte(nil), el.Octets...)
		default:
			if err := skipValue(dec, el); err != nil {
				return req, err
			}
		}
	}
}

// rewriteInvokeResponseCommand updates ent.Path.Command from the
// request command ID to the response command ID per Matter §10.6.7
// — chip-tool's TypedCommandCallback decodes the response payload by
// looking up the path's command ID in its response-type table, so
// echoing the request command ID makes it surface
// `CHIP_ERROR_SCHEMA_MISMATCH` even on a structurally valid payload.
//
// Only applied when ent has a Response (status-only entries already
// carry the request command ID by spec).
func rewriteInvokeResponseCommand(ent *im.InvokeResponseEntry) {
	if ent.IsStatus || ent.Response == nil {
		return
	}
	switch x := ent.Response.(type) {
	case mattercore.ArmFailSafeResponse:
		ent.Path.Command = 0x01
	case mattercore.SetRegulatoryConfigResponse:
		ent.Path.Command = 0x03
	case mattercore.CommissioningCompleteResponse:
		ent.Path.Command = 0x05
	case mattercore.AttestationResponse:
		ent.Path.Command = 0x01
	case mattercore.CertificateChainResponse:
		ent.Path.Command = 0x03
	case mattercore.CSRResponse:
		ent.Path.Command = 0x05
	case mattercore.NOCResponse:
		ent.Path.Command = 0x08
	case mattercore.SignVidVerificationResponse:
		ent.Path.Command = 0x0E
	case mattercore.KeySetReadResponse:
		ent.Path.Command = 0x02
	case mattercore.KeySetReadAllIndicesResponse:
		ent.Path.Command = 0x05
	case mattercore.LeaveGroupResponse:
		ent.Path.Command = 0x02
	case wire.OperationalCommandResponse:
		ent.Path.Command = opdef.CmdOperationalCommandResponse
	case wire.ChangeToModeResponse:
		ent.Path.Command = wire.ModeBaseCmdChangeToModeResponse
	case spec.ResponsePayload:
		// A response payload of a generated cluster definition names its
		// own response command.
		_, ent.Path.Command = x.ResponseCommand()
	}
	// Unknown response types (status-only commands wrapped) leave
	// the path alone — the writer emits an empty struct + the
	// request command ID, which chip-tool tolerates for void
	// responses.
}

// drainContainer consumes elements until the matching EndContainer.
// Caller has already entered the container via dec.Next() returning
// a TLV container element.
func drainContainer(dec *tlv.Decoder) error {
	depth := 1
	for depth > 0 {
		el, err := dec.Next()
		if err != nil {
			return err
		}
		if el.IsContainer {
			depth++
		}
		if el.IsEndContainer {
			depth--
		}
	}
	return nil
}
