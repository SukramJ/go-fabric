// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Joint Fabric end to end: a device of the administrator kit is
// commissioned as the anchor of its own fabric B, then by another
// commissioner with Joint Commissioning — which reads the device's
// administrator, verifies its vendor id over the commissioning session
// and asks for consent — and a refused consent aborts the commissioning.
package jointfabric_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/jcm"
	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/jointfabric"
	jfadef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricadministrator"
	dsdef "github.com/SukramJ/go-fabric/cluster/spec/jointfabricdatastore"
	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

const (
	adminCAT  uint32 = 0xFFFF_0001
	anchorCAT uint32 = 0xFFFE_0001
)

// nopSync answers every push and fetch empty.
type nopSync struct{}

func (nopSync) SyncEndpointGroupID(context.Context, uint64, jointfabric.EndpointGroupIDEntry) error {
	return nil
}

func (nopSync) SyncNodeKeySet(context.Context, uint64, jointfabric.NodeKeySetEntry) error { return nil }

func (nopSync) SyncBinding(context.Context, uint64, jointfabric.EndpointBindingEntry) error {
	return nil
}

func (nopSync) SyncBindings(context.Context, uint64, []jointfabric.EndpointBindingEntry) error {
	return nil
}

func (nopSync) SyncACL(context.Context, uint64, jointfabric.ACLEntry) error { return nil }

func (nopSync) SyncACLs(context.Context, uint64, []jointfabric.ACLEntry) error { return nil }

func (nopSync) SyncGroupKeySet(context.Context, uint64, jointfabric.GroupKeySet) error { return nil }

func (nopSync) FetchEndpointList(context.Context, uint64) ([]jointfabric.EndpointEntry, error) {
	return nil, nil
}

func (nopSync) FetchEndpointGroupList(context.Context, uint64, uint16) ([]jointfabric.GroupEntry, error) {
	return nil, nil
}

func (nopSync) FetchEndpointBindingList(context.Context, uint64, uint16) ([]jointfabric.EndpointBindingEntry, error) {
	return nil, nil
}
func (nopSync) FetchGroupKeySetList(context.Context, uint64) ([]uint16, error) { return nil, nil }
func (nopSync) FetchGroupKeySet(context.Context, uint64, uint16) (jointfabric.GroupKeySet, error) {
	return jointfabric.GroupKeySet{}, nil
}

func (nopSync) FetchACLList(context.Context, uint64) ([]jointfabric.ACLEntry, error) { return nil, nil }

// keyPort is the administrator key's CSR.
type keyPort struct{ csr []byte }

func (p keyPort) IcacCsr(context.Context) ([]byte, error) { return p.csr, nil }

func newKeyPort(t *testing.T) keyPort {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	return keyPort{csr: csr}
}

func TestJointCommissioningOverTheWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	jfa, err := device.NewJointFabricAdministrator(device.Options{StableKey: endpoint.StringKey("jfa"), Name: "jfa"},
		dsdef.Declare{Sync: nopSync{}}, jfadef.Declare{AdministratorPort: newKeyPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := jfa.Server(dsdef.ClusterID).(*jointfabric.Datastore)
	admin, _ := jfa.Server(jfadef.ClusterID).(*jointfabric.Administrator)
	if ds == nil || admin == nil {
		t.Fatal("the kit did not mount the Joint Fabric servers")
	}
	site := fabrictest.NewSite(t, fabrictest.Options{Clock: clock.System{}})
	dev := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{jfa}})

	// The anchor: fabric B's commissioner issues the administrator and
	// anchor CATs; at CommissioningComplete the administrator takes the
	// fabric and the datastore records the anchor.
	b := site.AddController(fabrictest.ControllerOptions{FabricID: 0x0B, VendorID: 0xFFF2, IntermediateCA: true})
	pairB, err := b.Commission(ctx, dev, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) { o.CATs = []uint32{adminCAT, anchorCAT} }})
	if err != nil {
		t.Fatalf("commission onto B: %v", err)
	}
	if err := site.Eventually(ctx, "the anchor recorded", func() bool { return ds.AnchorFabricIndex() != 0 }); err != nil {
		t.Fatal(err)
	}
	if ds.AnchorNodeID() != pairB.Address.NodeID || ds.AnchorVendorID() != 0xFFF2 || len(ds.AnchorRootCA()) == 0 {
		t.Fatalf("anchor node %#x vendor %#x root %d bytes", ds.AnchorNodeID(), ds.AnchorVendorID(), len(ds.AnchorRootCA()))
	}

	// Joint Commissioning refused: the trust verification runs, the host
	// says no, nothing is commissioned.
	if err := dev.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	c := site.AddController(fabrictest.ControllerOptions{FabricID: 0x0C})
	var refused commissioner.TrustInfo
	_, err = c.Commission(ctx, dev, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
		o.JointFabric = &commissioner.JointFabricOptions{Consent: func(info commissioner.TrustInfo) bool { refused = info; return false }}
	}})
	if !errors.Is(err, commissioner.ErrJointFabric) {
		t.Fatalf("refused consent: %v, want ErrJointFabric", err)
	}
	if refused.AdminVendorID != 0xFFF2 || refused.AdminFabricID != 0x0B {
		t.Fatalf("consent asked with %+v", refused)
	}

	// Joint Commissioning with consent: the device joins the fabric with
	// the administrator CAT.
	if err := dev.OpenWindow(ctx, 0); err != nil {
		t.Fatal(err)
	}
	a := site.AddController(fabrictest.ControllerOptions{FabricID: 0x0A})
	var asked bool
	var steps []commissioner.Step
	var stages []jcm.Stage
	pairA, err := a.Commission(ctx, dev, fabrictest.CommissionOptions{Edit: func(o *commissioner.Options) {
		o.CATs = []uint32{adminCAT}
		o.Progress = func(s commissioner.Step) { steps = append(steps, s) }
		o.JointFabric = &commissioner.JointFabricOptions{
			Consent:  func(commissioner.TrustInfo) bool { asked = true; return true },
			Progress: func(s jcm.Stage, _ jcm.Result) { stages = append(stages, s) },
		}
	}})
	if err != nil {
		t.Fatalf("joint commissioning: %v", err)
	}
	if !asked || !pairA.Commissioned() {
		t.Fatalf("consent asked %v, commissioned %v", asked, pairA.Commissioned())
	}
	var order []string
	for _, s := range steps {
		if s.Phase == commissioner.StepSucceeded {
			order = append(order, s.Name)
		}
	}
	i := slices.Index(order, commissioner.StepJointFabric)
	if i < 1 || order[i-1] != "OperationalCredentials.DeviceAttestation" || order[i+1] != "OperationalCredentials.Certificates" {
		t.Fatalf("steps %v", order)
	}
	if !slices.Equal(stages, []jcm.Stage{jcm.StageIdle, jcm.StageVerifyingAdministratorInformation, jcm.StagePerformingVendorIDVerification, jcm.StageAskingUserForConsent, jcm.StageComplete}) {
		t.Fatalf("stages %v", stages)
	}
}
