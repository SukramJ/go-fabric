// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"

	"github.com/SukramJ/go-fabric/cluster/sensing"
	acsdef "github.com/SukramJ/go-fabric/cluster/spec/ambientcontextsensing"
	"github.com/SukramJ/go-fabric/contract"
)

// The kit's AmbientContextSensing server (cluster/sensing, brief 26-C).
// It needs no port — chip's delegate is the server's own state and the
// device's clock — so it is the default wherever a device type lists the
// cluster, with the declaration's feature selection and its Initial as
// the configuration ([configFrom]); a selection without HA, OC, OI or AUD
// keeps the generated server. No device type at the pin lists the cluster
// as a server: matter.js has it only as the thermostat's client
// (AmbientContextSensingClient, packages/node/src/devices/thermostat.ts:133-138
// at 862ad75f). A host mounts it on another type as the declaration's
// HostServer (acsdef.Declare{HostServer: srv} with
// [sensing.NewAmbientContext]), the route for a cluster a type does not
// list ([WithServer]).

func init() {
	selfHeldDefaults[acsdef.ClusterID] = newAmbientContextServer
}

// newAmbientContextServer is the module's AmbientContextSensing server
// ([sensing.NewAmbientContext]); HoldTime, HoldTimeLimits,
// ObjectCountConfig, SimultaneousDetectionLimit and
// AmbientContextTypeSupported start from Initial.
func newAmbientContextServer(b defaultBuild) (contract.ClusterServer, error) {
	cfg := sensing.AmbientContextConfig{
		Features:         sensing.AmbientContextFeature(b.opts.Features),
		ServeObjectCount: slices.Contains(b.opts.Attributes, acsdef.AttrObjectCount),
		Clock:            b.clock, DataVersion: b.cfg.DataVersion,
	}
	configFrom(&cfg, b.def, b.initial)
	srv, err := sensing.NewAmbientContext(cfg)
	if err != nil {
		return nil, notServed(err)
	}
	return srv, nil
}
