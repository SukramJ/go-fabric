// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	fixedlabeldef "github.com/SukramJ/go-fabric/cluster/spec/fixedlabel"
)

// FixedLabel declares a FixedLabel server with labels on a device: the
// cluster the Base device type allows on every endpoint as optional
// (matter.js packages/model/src/standard/elements/base.element.ts:46 at
// 862ad75f), which a device type rarely lists itself. It is
// fixedlabeldef.Declare{HostServer: …} over the module's FixedLabel
// server (cluster/core.NewFixedLabel), so a host states the labels and
// imports no server package; a label set the server refuses (an over-long
// label or value) is the error.
func FixedLabel(labels ...fixedlabeldef.LabelStruct) (Declaration, error) {
	srv, err := mattercore.NewFixedLabel(labels)
	if err != nil {
		return nil, err
	}
	return fixedlabeldef.Declare{HostServer: srv}, nil
}
