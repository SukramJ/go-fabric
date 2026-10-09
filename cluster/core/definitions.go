// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// mustInstance binds a generated definition (cluster/spec/<name>, ADR
// 0013) to the fixed selection a server of this package serves. The
// selections are constants of the package, so an error is a programming
// error its tests catch at init.
func mustInstance(def *spec.Cluster, opts spec.Options) *spec.Instance {
	inst, err := spec.New(def, opts)
	if err != nil {
		panic(fmt.Sprintf("core: %v", err))
	}
	return inst
}

// refusedWrite is a write the definition refuses (status: UNSUPPORTED_WRITE
// for a served read-only attribute, UNSUPPORTED_ATTRIBUTE otherwise, as
// matter.js AttributeWriteResponse answers them) with the server's own
// sentinel kept as the error it unwraps to.
type refusedWrite struct {
	sentinel error
	status   error
}

func (e refusedWrite) Error() string {
	if e.status == nil {
		return e.sentinel.Error()
	}
	return e.sentinel.Error() + ": " + e.status.Error()
}

func (e refusedWrite) Unwrap() error { return e.sentinel }

// MatterStatusCode implements [im.StatusCodeError].
func (e refusedWrite) MatterStatusCode() im.StatusCode {
	if sce, ok := errors.AsType[im.StatusCodeError](e.status); ok {
		return sce.MatterStatusCode()
	}
	return im.StatusFailure
}
