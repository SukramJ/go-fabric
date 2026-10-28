// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

// The host ports of the Joint Fabric servers (brief 26-D): the
// Datastore's sync to the member nodes and its persistence, the
// Administrator's ICAC CSR and its reach back to the commissioner that
// announces itself. cluster/jointfabric names each by alias. The module
// holds the tables, the refresh, the commands' rules and the trust
// verification; the host's admin application reaches the nodes and holds
// the administrator key.
var jointFabricPorts = map[string]portSet{
	"JointFabricDatastore": {ports: []portDecl{{
		name: "DatastoreSync",
		doc: []string{
			"DatastoreSync carries the datastore's entries to the member nodes",
			"and fetches their lists for a refresh — connectedhomeip's",
			"JointFabricDatastore::Delegate (JointFabricDatastore.h:166-285 at",
			"6170af84). Each Sync writes one entry (an entry whose StatusEntry",
			"is DeletePending removes it; a zero entry with only the ids set",
			"removes it too) or, for the bindings and the ACLs, the node's whole",
			"list; SyncBindings may mark an entry of the list CommitFailed with",
			"its FailureCode in place. An error leaves the entry as it was",
			"(Pending or DeletePending) for the next RefreshNode. Each Fetch",
			"reads the node's current list; an error ends the refresh with the",
			"node Pending. The datastore calls it outside its lock.",
		},
		field: "Sync",
		methods: []string{
			"SyncEndpointGroupID(ctx context.Context, nodeID uint64, entry DatastoreEndpointGroupIDEntryStruct) error",
			"SyncNodeKeySet(ctx context.Context, nodeID uint64, entry DatastoreNodeKeySetEntryStruct) error",
			"SyncBinding(ctx context.Context, nodeID uint64, entry DatastoreEndpointBindingEntryStruct) error",
			"SyncBindings(ctx context.Context, nodeID uint64, entries []DatastoreEndpointBindingEntryStruct) error",
			"SyncACL(ctx context.Context, nodeID uint64, entry DatastoreACLEntryStruct) error",
			"SyncACLs(ctx context.Context, nodeID uint64, entries []DatastoreACLEntryStruct) error",
			"SyncGroupKeySet(ctx context.Context, nodeID uint64, keySet DatastoreGroupKeySetStruct) error",
			"FetchEndpointList(ctx context.Context, nodeID uint64) ([]DatastoreEndpointEntryStruct, error)",
			"FetchEndpointGroupList(ctx context.Context, nodeID uint64, endpointID uint16) ([]DatastoreGroupInformationEntryStruct, error)",
			"FetchEndpointBindingList(ctx context.Context, nodeID uint64, endpointID uint16) ([]DatastoreEndpointBindingEntryStruct, error)",
			"FetchGroupKeySetList(ctx context.Context, nodeID uint64) ([]uint16, error)",
			"FetchGroupKeySet(ctx context.Context, nodeID uint64, groupKeySetID uint16) (DatastoreGroupKeySetStruct, error)",
			"FetchACLList(ctx context.Context, nodeID uint64) ([]DatastoreACLEntryStruct, error)",
		},
	}, {
		name: "DatastoreStore",
		doc: []string{
			"DatastoreStore persists the datastore's tables as one settings",
			"record under the device's StableKey; the node's store",
			"(node.Node.Store) is one. Without one the tables stay in memory, as",
			"connectedhomeip keeps them (JointFabricDatastore.h:562, \"TODO:",
			"Persist these members\").",
		},
		field: "Store",
		methods: []string{
			"GetSetting(ctx context.Context, key string) (value string, ok bool, err error)",
			"SetSetting(ctx context.Context, key, value string) error",
		},
	}}},
	"JointFabricAdministrator": {ports: []portDecl{{
		name: "AdministratorPort",
		doc: []string{
			"AdministratorPort is the administrator application's key: IcacCsr",
			"returns the PKCS#10 CSR of the key the administrator's ICAC will",
			"certify — connectedhomeip's JointFabricAdministrator::Delegate",
			"GetIcacCsr (JointFabricAdministrator.h:34-41 at 6170af84). The",
			"server answers ICACCSRRequest with it and checks AddICAC's public",
			"key against it.",
		},
		methods: []string{
			"IcacCsr(ctx context.Context) ([]byte, error)",
		},
	}, {
		name: "CommissionerReach",
		doc: []string{
			"CommissionerReach reads the administrator data of the commissioner",
			"that invoked AnnounceJointFabricAdministrator and has it sign the",
			"Vendor ID Verification — connectedhomeip's JCMCommissionee reads over",
			"the invoking session (JCMCommissionee.cpp:46-69, :297-322 at",
			"6170af84). ctx is the invoke's context, which names the peer",
			"(im.SubjectFromContext, im.FabricFilterFromContext); the reads are",
			"not fabric-filtered. Without one AnnounceJointFabricAdministrator",
			"fails.",
		},
		methods: []string{
			"ReadAdministratorFabricIndex(ctx context.Context, endpointID uint16) (fabricIndex uint8, null bool, err error)",
			"ReadFabrics(ctx context.Context) ([]operationalcredentials.FabricDescriptorStruct, error)",
			"ReadTrustedRootCertificates(ctx context.Context) ([][]byte, error)",
			"ReadNOCs(ctx context.Context) ([]operationalcredentials.NOCStruct, error)",
			"SignVIDVerification(ctx context.Context, fabricIndex uint8, clientChallenge []byte) (resp operationalcredentials.SignVidVerificationResponse, attestationChallenge []byte, err error)",
		},
	}}, imports: []string{"github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"}},
}

func init() {
	for name, ps := range jointFabricPorts {
		ports[name] = ps
	}
}
