// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"

	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	jfdsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/surface"
)

// --jointfabric-sim (TESTING ONLY): a JointFabricAdministrator endpoint
// (device type 0x0130: JointFabricDatastore and JointFabricAdministrator,
// cluster/jointfabric) for the JFADMIN and JFDS certification families,
// after connectedhomeip's jf-admin-app at the harness pin 6170af84
// (examples/jf-admin-app: one administrator endpoint, jfa-app.matter
// endpoint 1). The administrator's key behind AdministratorPort is a
// software P-256 key kept in the daemon's settings (jfKeySetting); the
// datastore's tables persist in the node's store (DatastoreStore); the
// member-node synchronisation (DatastoreSync) is a simulation that
// reaches no node: it logs each request and answers the fetches with
// nothing, as a datastore whose nodes expose nothing. The commissioner
// reach — the reads of an announcing commissioner — is the node's
// (node.Config.Client turned on).
//
// chip's JF cases drive the administrator through chip's jf-control-app
// and the administrator's pigweed RPC JointFabric service
// (examples/common/pigweed/protos/joint_fabric_service.proto; the
// non-CI path of TC_JFADMIN_1_1.py names dut_rpc_server_ip and _port);
// the daemon serves no pigweed RPC, so docs/certifiability.md classifies
// what that leaves out.

// jfKeySetting is the settings key the administrator key is kept under.
const jfKeySetting = "jointfabricsim.key"

// demoJointFabric is the --jointfabric-sim endpoint and its simulations.
type demoJointFabric struct {
	logger  *slog.Logger
	surface surface.Policies

	mu  sync.Mutex
	st  settings
	key *ecdsa.PrivateKey
	dev *device.JointFabricAdministrator
}

func newDemoJointFabric(policies surface.Policies, logger *slog.Logger) *demoJointFabric {
	return &demoJointFabric{logger: logger, surface: policies}
}

// applyJointFabric turns on the node's client stack, which the
// commissioner reach needs.
func applyJointFabric(cfg *node.Config) {
	if cfg.Client == nil {
		cfg.Client = &node.ClientConfig{}
	}
}

// attach binds the simulation to the node's settings; call it before
// devices.
func (j *demoJointFabric) attach(st settings) {
	j.mu.Lock()
	j.st = st
	j.mu.Unlock()
}

// devices builds the administrator endpoint.
func (j *demoJointFabric) devices() ([]node.Endpoint, error) {
	if j.dev == nil {
		o := bridged("jointfabric", "Joint Fabric Administrator")
		o.Surface = j.surface
		j.mu.Lock()
		st := j.st
		j.mu.Unlock()
		decl := jfdsdef.Declare{Sync: jfSync{logger: j.logger}}
		if st != nil {
			decl.Store = st
		}
		d, err := device.NewJointFabricAdministrator(o, decl, jfadef.Declare{AdministratorPort: j})
		if err != nil {
			return nil, fmt.Errorf("joint fabric administrator: %w", err)
		}
		j.dev = d
	}
	return []node.Endpoint{j.dev}, nil
}

// IcacCsr is the PKCS#10 request of the administrator key, made once and
// kept (jf-admin-app's GetIcacCsr hands its key's CSR, JFAManager.cpp).
func (j *demoJointFabric) IcacCsr(ctx context.Context) ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.key == nil {
		j.loadKeyLocked(ctx)
	}
	if j.key == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		j.key = key
		if der, err := x509.MarshalECPrivateKey(key); err == nil && j.st != nil {
			if err := j.st.SetSetting(context.WithoutCancel(ctx), jfKeySetting, base64.StdEncoding.EncodeToString(der)); err != nil {
				j.logger.Warn("jointfabricsim.persist", slog.Any("err", err))
			}
		}
	}
	return x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{SignatureAlgorithm: x509.ECDSAWithSHA256}, j.key)
}

func (j *demoJointFabric) loadKeyLocked(ctx context.Context) {
	if j.st == nil {
		return
	}
	v, ok, err := j.st.GetSetting(ctx, jfKeySetting)
	if err != nil || !ok {
		return
	}
	der, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		j.key = key
	}
}

// jfSync is the DatastoreSync of --jointfabric-sim: it reaches no member
// node, logs each request and fetches nothing.
type jfSync struct{ logger *slog.Logger }

func (s jfSync) log(op string, nodeID uint64) error {
	s.logger.Info("jointfabricsim.sync", slog.String("op", op), slog.Uint64("node", nodeID))
	return nil
}

func (s jfSync) SyncEndpointGroupID(_ context.Context, n uint64, _ jfdsdef.DatastoreEndpointGroupIDEntryStruct) error {
	return s.log("SyncEndpointGroupID", n)
}

func (s jfSync) SyncNodeKeySet(_ context.Context, n uint64, _ jfdsdef.DatastoreNodeKeySetEntryStruct) error {
	return s.log("SyncNodeKeySet", n)
}

func (s jfSync) SyncBinding(_ context.Context, n uint64, _ jfdsdef.DatastoreEndpointBindingEntryStruct) error {
	return s.log("SyncBinding", n)
}

func (s jfSync) SyncBindings(_ context.Context, n uint64, _ []jfdsdef.DatastoreEndpointBindingEntryStruct) error {
	return s.log("SyncBindings", n)
}

func (s jfSync) SyncEndpointBindings(_ context.Context, n uint64, _ uint16, _ []jfdsdef.DatastoreEndpointBindingEntryStruct) error {
	return s.log("SyncEndpointBindings", n)
}

func (s jfSync) SyncACL(_ context.Context, n uint64, _ jfdsdef.DatastoreACLEntryStruct) error {
	return s.log("SyncACL", n)
}

func (s jfSync) SyncACLs(_ context.Context, n uint64, _ []jfdsdef.DatastoreACLEntryStruct) error {
	return s.log("SyncACLs", n)
}

func (s jfSync) SyncGroupKeySet(_ context.Context, n uint64, _ jfdsdef.DatastoreGroupKeySetStruct) error {
	return s.log("SyncGroupKeySet", n)
}

func (s jfSync) FetchEndpointList(_ context.Context, n uint64) ([]jfdsdef.DatastoreEndpointEntryStruct, error) {
	return nil, s.log("FetchEndpointList", n)
}

func (s jfSync) FetchEndpointGroupList(_ context.Context, n uint64, _ uint16) ([]jfdsdef.DatastoreGroupInformationEntryStruct, error) {
	return nil, s.log("FetchEndpointGroupList", n)
}

func (s jfSync) FetchEndpointBindingList(_ context.Context, n uint64, _ uint16) ([]jfdsdef.DatastoreEndpointBindingEntryStruct, error) {
	return nil, s.log("FetchEndpointBindingList", n)
}

func (s jfSync) FetchGroupKeySetList(_ context.Context, n uint64) ([]uint16, error) {
	return nil, s.log("FetchGroupKeySetList", n)
}

func (s jfSync) FetchGroupKeySet(_ context.Context, n uint64, _ uint16) (jfdsdef.DatastoreGroupKeySetStruct, error) {
	return jfdsdef.DatastoreGroupKeySetStruct{}, s.log("FetchGroupKeySet", n)
}

func (s jfSync) FetchACLList(_ context.Context, n uint64) ([]jfdsdef.DatastoreACLEntryStruct, error) {
	return nil, s.log("FetchACLList", n)
}
