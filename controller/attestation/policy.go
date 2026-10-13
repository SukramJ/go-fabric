// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation

import "log/slog"

// Policy decides whether commissioning proceeds on attestation findings —
// matter.js DeviceAttestationValidator.OnAttestationFailure (DAV:142-156)
// as ControllerCommissioningFlow applies it (ControllerCommissioningFlow.ts:
// 129-151, #resolveAttestationFindings :1341-1368 at 0ceb202d). The
// commissioner applies it; [Validate] does not.
//
// The zero Policy is matter.js's undefined: every finding is accepted with
// a warning — the backward-compatible default matter.js marks for removal
// (:149, :1355).
type Policy struct {
	// OnFailure is the boolean form: true proceeds (each finding logged as
	// accepted), false rejects. Nil is undefined.
	OnFailure *bool
	// Decide is the function form; when set it alone decides (:1344-1350):
	// proceed true proceeds; otherwise a non-empty reason rejects with a
	// commissioning error of that message caused by the underlying error,
	// an empty one rejects with the underlying error; a non-nil err rejects
	// with err verbatim.
	Decide func(findings []Finding) (proceed bool, reason string, err error)
}

// Accept is the policy `true`.
func Accept() Policy { t := true; return Policy{OnFailure: &t} }

// Reject is the policy `false`.
func Reject() Policy { f := false; return Policy{OnFailure: &f} }

// Resolve applies the policy to findings — matter.js
// ControllerCommissioningFlow.#resolveAttestationFindings
// (ControllerCommissioningFlow.ts:1341-1368 at 0ceb202d): the function form
// decides alone (:1344-1350); otherwise each finding is logged — a warning
// under the undefined policy, info under true and false — and the
// commissioning proceeds unless the policy is false (:1352-1367). err is
// the function's own error, to be returned verbatim.
func (p Policy) Resolve(findings []Finding, log *slog.Logger) (proceed bool, reason string, err error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if p.Decide != nil {
		return p.Decide(findings)
	}
	for _, f := range findings {
		attrs := []any{slog.String("check", string(f.Check)), slog.String("message", f.Message)}
		switch {
		case p.OnFailure == nil:
			log.Warn("matter.controller.attestation.finding_accepted_for_backward_compatibility", attrs...)
		case *p.OnFailure:
			log.Info("matter.controller.attestation.finding_accepted_by_policy", attrs...)
		default:
			log.Info("matter.controller.attestation.finding_rejecting", attrs...)
		}
	}
	return p.OnFailure == nil || *p.OnFailure, "", nil
}
