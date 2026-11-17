// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import "github.com/SukramJ/go-fabric/client/jcm"

// SetTrustAnchorsForTest gives an administrator the device kit built the
// trust anchor lookup its declaration cannot carry: the site's fabrics are
// self-minted, so a test anchors the vendor id at the commissioner's own
// root (jcm.SelfAnchored). Before any joint commissioning runs.
func (a *Administrator) SetTrustAnchorsForTest(lookup jcm.TrustAnchorLookup) {
	a.trustAnchors = lookup
}
