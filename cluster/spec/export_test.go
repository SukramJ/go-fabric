// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

// SetWindowHooksForTests installs s's test hooks: stored runs in a Set
// between its store and its notification, left in a controller write
// between leaving its commit window and advancing the version. Call it
// before s is used.
func SetWindowHooksForTests(s *Server, stored, left func()) {
	s.storedForTests, s.leftForTests = stored, left
}

// RequestErrorsForTests is every error the request gate finds in the
// request fields of cmdID ([Instance.admitRequest]'s collector).
func RequestErrorsForTests(i *Instance, cmdID uint32, fields any) []error {
	return i.requestErrors(cmdID, fields, nil, true)
}

// SetJudgedHookForTests installs judged, which runs in a Set after its
// values were judged against the state and before they are stored. Call
// it before s is used.
func SetJudgedHookForTests(s *Server, judged func()) {
	s.judgedForTests = judged
}
