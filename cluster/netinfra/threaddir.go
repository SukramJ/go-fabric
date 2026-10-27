// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netinfra

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdir "github.com/SukramJ/go-fabric/cluster/spec/threadnetworkdirectory"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/thread"
	"github.com/SukramJ/go-fabric/im"
)

// ThreadNetworkDirectory (0x0453) after connectedhomeip
// src/app/clusters/thread-network-directory-server/
// ThreadNetworkDirectoryCluster.cpp at 6170af84, cited below by line; the
// store after ThreadNetworkDirectoryStorage.h there.

// ThreadNetwork is the ThreadNetworkStruct of the generated definition.
type ThreadNetwork = tdir.ThreadNetworkStruct

// ThreadNetworkStore is the host's persistence of the directory's
// datasets (= threadnetworkdirectorydef.ThreadNetworkStore, generated,
// Declare.ThreadNetworkStore), in the spirit of chip's
// ThreadNetworkDirectoryStorage (ThreadNetworkDirectoryStorage.h:31-124):
// Capacity is the table size (:73), LoadNetworks the stored datasets in a
// stable order (:86, :95), SaveNetwork adds or replaces the dataset of an
// Extended PAN ID (:106) and RemoveNetwork drops one (:114). The server
// validates every dataset before it saves it, treats it as an opaque blob
// otherwise (:98-100) and checks the capacity and the presence itself, so
// a store has no sentinel errors to answer; an error it returns answers
// the command FAILURE and changes nothing. The calls run under the
// server's lock: the store must not call back into the server.
type ThreadNetworkStore = tdir.ThreadNetworkStore

// ExtendedPanIDLen is the size of a Thread Extended PAN ID
// (ThreadNetworkDirectoryStorage.h:43-46).
const ExtendedPanIDLen = 8

// maxDatasetLen is the largest operational dataset
// (ThreadNetworkDirectoryStorage.h:34, kMaxThreadDatasetLen; the AddNetwork
// field's constraint "max 254").
const maxDatasetLen = 254

// maxNetworkNameLen is kSizeNetworkName (src/lib/support/
// ThreadOperationalDataset.h:38).
const maxNetworkNameLen = 16

// pskcLen is kSizePSKc (ThreadOperationalDataset.h:42).
const pskcLen = 16

// DefaultThreadNetworkCapacity is the capacity of the in-memory store:
// chip's default CHIP_CONFIG_MAX_THREAD_NETWORK_DIRECTORY_STORAGE_CAPACITY,
// twice CHIP_CONFIG_MAX_FABRICS (src/lib/core/CHIPConfig.h:1891, :799).
const DefaultThreadNetworkCapacity = 32

// ErrStoreFull is the in-memory store refusing a new network beyond its
// capacity (DefaultThreadNetworkDirectoryStorage.cpp:92, NO_MEMORY).
var ErrStoreFull = errors.New("netinfra: the Thread network store is full")

// ErrInvalidThreadNetworks is a store whose datasets the server cannot
// serve: one that is not a dataset AddNetwork accepts, two of the same
// Extended PAN ID, or more than the capacity.
var ErrInvalidThreadNetworks = errors.New("netinfra: the Thread network store holds invalid datasets")

// MemoryThreadNetworkStore is a [ThreadNetworkStore] in memory, the
// server's default: networks in the order they were first added, an
// update keeping its place.
type MemoryThreadNetworkStore struct {
	capacity uint8

	// mu guards ids and datasets.
	mu       sync.Mutex
	ids      []string
	datasets map[string][]byte
}

// NewMemoryThreadNetworkStore returns an empty in-memory store of
// capacity networks; 0 is [DefaultThreadNetworkCapacity].
func NewMemoryThreadNetworkStore(capacity uint8) *MemoryThreadNetworkStore {
	if capacity == 0 {
		capacity = DefaultThreadNetworkCapacity
	}
	return &MemoryThreadNetworkStore{capacity: capacity, datasets: map[string][]byte{}}
}

// Capacity implements [ThreadNetworkStore].
func (m *MemoryThreadNetworkStore) Capacity() uint8 { return m.capacity }

// LoadNetworks implements [ThreadNetworkStore].
func (m *MemoryThreadNetworkStore) LoadNetworks(context.Context) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, 0, len(m.ids))
	for _, id := range m.ids {
		out = append(out, slices.Clone(m.datasets[id]))
	}
	return out, nil
}

// SaveNetwork implements [ThreadNetworkStore]: [ErrStoreFull] for a new
// network beyond the capacity, as chip's default storage refuses one
// (DefaultThreadNetworkDirectoryStorage.cpp:90-92).
func (m *MemoryThreadNetworkStore) SaveNetwork(_ context.Context, extendedPanID, dataset []byte) error {
	if len(extendedPanID) != ExtendedPanIDLen || len(dataset) == 0 || len(dataset) > maxDatasetLen {
		return fmt.Errorf("netinfra: SaveNetwork: Extended PAN ID of %d bytes or dataset of %d bytes", len(extendedPanID), len(dataset))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := string(extendedPanID)
	if _, ok := m.datasets[id]; !ok {
		if len(m.ids) >= int(m.capacity) {
			return ErrStoreFull
		}
		m.ids = append(m.ids, id)
	}
	m.datasets[id] = slices.Clone(dataset)
	return nil
}

// RemoveNetwork implements [ThreadNetworkStore]; a network not stored is
// no error.
func (m *MemoryThreadNetworkStore) RemoveNetwork(_ context.Context, extendedPanID []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := string(extendedPanID)
	delete(m.datasets, id)
	m.ids = slices.DeleteFunc(m.ids, func(s string) bool { return s == id })
	return nil
}

// ThreadDirectoryConfig carries the ThreadNetworkDirectory server's
// construction parameters.
type ThreadDirectoryConfig struct {
	// Store persists the datasets; a [MemoryThreadNetworkStore] of
	// [DefaultThreadNetworkCapacity] when nil.
	Store ThreadNetworkStore
	// PreferredExtendedPanID is the preferred network the server starts
	// with, nil for none; it must name a stored network.
	PreferredExtendedPanID []byte
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
	// Logger receives the refusals chip logs (ChipLogError, :274, :295,
	// :305, :333); slog.Default when nil. A dataset is never logged.
	Logger *slog.Logger
}

// directoryEntry is one stored network: its dataset and what
// ThreadNetworks and the timestamp check read from it.
type directoryEntry struct {
	dataset   []byte
	network   ThreadNetwork
	timestamp uint64
}

// ThreadDirectoryServer implements [contract.ClusterServer] for
// ThreadNetworkDirectory: AddNetwork, RemoveNetwork and
// GetOperationalDataset with chip's checks and status codes, and the
// writable PreferredExtendedPanID, which must name a stored network
// (:139-151). ThreadNetworks and ThreadNetworkTableSize are the store's.
// The preferred network is server state: a host persists it from the
// change reports and hands it back as [ThreadDirectoryConfig]'s
// PreferredExtendedPanID (chip keeps it in attribute storage, :113).
type ThreadDirectoryServer struct {
	*spec.Instance
	// SelfHeld is the typed server's command handlers, write veto and
	// SetAttributes over srv.
	spec.SelfHeld

	srv    *spec.Server
	store  ThreadNetworkStore
	logger *slog.Logger

	// mu guards order and entries and serialises the commands, the
	// preferred network's writes and the store's calls.
	mu      sync.Mutex
	order   []string
	entries map[string]directoryEntry
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*ThreadDirectoryServer)(nil)
	_ contract.ClusterDataVersion             = (*ThreadDirectoryServer)(nil)
	_ contract.ClusterAttributeLister         = (*ThreadDirectoryServer)(nil)
	_ contract.ClusterCommandLister           = (*ThreadDirectoryServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*ThreadDirectoryServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*ThreadDirectoryServer)(nil)
	_ contract.AttributeChangeNotifier        = (*ThreadDirectoryServer)(nil)
)

// NewThreadDirectory builds a ThreadNetworkDirectory server over the
// store's networks: ThreadNetworkTableSize is its capacity (:83-84),
// ThreadNetworks its datasets (:153-183), FeatureMap 0 (:89-90). A store
// that fails to load or holds a dataset the server would not accept is
// [ErrInvalidThreadNetworks]; a preferred network it does not hold is
// refused.
func NewThreadDirectory(ctx context.Context, cfg ThreadDirectoryConfig) (*ThreadDirectoryServer, error) {
	s := &ThreadDirectoryServer{store: cfg.Store, logger: cfg.Logger}
	if s.store == nil {
		s.store = NewMemoryThreadNetworkStore(0)
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	var preferred any
	if cfg.PreferredExtendedPanID != nil {
		if _, ok := s.entries[string(cfg.PreferredExtendedPanID)]; !ok {
			return nil, fmt.Errorf("netinfra: the preferred Extended PAN ID %X names no stored network", cfg.PreferredExtendedPanID)
		}
		preferred = slices.Clone(cfg.PreferredExtendedPanID)
	}
	srv, err := spec.NewServer(tdir.Definition, spec.Options{}, spec.ServerConfig{
		DataVersion: cfg.DataVersion,
		Initial: map[uint32]any{
			tdir.AttrPreferredExtendedPanId: preferred,
			tdir.AttrThreadNetworkTableSize: s.store.Capacity(),
			tdir.AttrThreadNetworks:         s.networksLocked(),
		},
		Handlers: map[uint32]spec.CommandHandler{
			tdir.CmdAddNetwork:            s.addNetwork,
			tdir.CmdRemoveNetwork:         s.removeNetwork,
			tdir.CmdGetOperationalDataset: s.getOperationalDataset,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("netinfra: %w", err)
	}
	s.Instance, s.SelfHeld, s.srv = srv.Instance, spec.NewSelfHeld(srv), srv
	return s, nil
}

// load reads the store into the server's table.
func (s *ThreadDirectoryServer) load(ctx context.Context) error {
	datasets, err := s.store.LoadNetworks(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidThreadNetworks, err)
	}
	if len(datasets) > int(s.store.Capacity()) {
		return fmt.Errorf("%w: %d networks, capacity %d", ErrInvalidThreadNetworks, len(datasets), s.store.Capacity())
	}
	order := make([]string, 0, len(datasets))
	entries := make(map[string]directoryEntry, len(datasets))
	for i, ds := range datasets {
		e, err := parseDataset(ds)
		if err != nil {
			return fmt.Errorf("%w: dataset %d: %w", ErrInvalidThreadNetworks, i, err)
		}
		id := string(e.network.ExtendedPanId)
		if _, dup := entries[id]; dup {
			return fmt.Errorf("%w: Extended PAN ID %X stored twice", ErrInvalidThreadNetworks, e.network.ExtendedPanId)
		}
		order = append(order, id)
		entries[id] = e
	}
	s.order, s.entries = order, entries
	return nil
}

// ReloadNetworks re-reads the store after the host changed it outside
// the cluster and reports ThreadNetworks when it moved. chip reads the
// storage on every read (:153-183); this server serves its table, so a
// host that writes the store directly calls this. A preferred network
// the store no longer holds is cleared.
func (s *ThreadDirectoryServer) ReloadNetworks(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(ctx); err != nil {
		return err
	}
	changes := map[uint32]any{}
	if cur, _ := s.srv.Value(tdir.AttrThreadNetworks); !sameNetworks(cur, s.networksLocked()) {
		changes[tdir.AttrThreadNetworks] = s.networksLocked()
	}
	if p, ok := s.preferredLocked(); ok {
		if _, held := s.entries[string(p)]; !held {
			changes[tdir.AttrPreferredExtendedPanId] = nil
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return s.srv.SetAttributes(changes)
}

// sameNetworks compares a stored ThreadNetworks value with a new list.
func sameNetworks(cur any, next spec.List[ThreadNetwork]) bool {
	list, ok := cur.(spec.List[ThreadNetwork])
	if !ok || len(list) != len(next) {
		return false
	}
	for i := range list {
		a, b := list[i], next[i]
		if !bytes.Equal(a.ExtendedPanId, b.ExtendedPanId) || a.NetworkName != b.NetworkName ||
			a.Channel != b.Channel || a.ActiveTimestamp != b.ActiveTimestamp {
			return false
		}
	}
	return true
}

// networksLocked is ThreadNetworks from the table, in the store's order
// (:155-178: the Extended PAN ID, the network name, the channel and the
// active timestamp of each dataset).
func (s *ThreadDirectoryServer) networksLocked() spec.List[ThreadNetwork] {
	out := make(spec.List[ThreadNetwork], 0, len(s.order))
	for _, id := range s.order {
		n := s.entries[id].network
		n.ExtendedPanId = slices.Clone(n.ExtendedPanId)
		out = append(out, n)
	}
	return out
}

// preferredLocked is the stored PreferredExtendedPanID; false for null.
func (s *ThreadDirectoryServer) preferredLocked() ([]byte, bool) {
	v, _ := s.srv.Value(tdir.AttrPreferredExtendedPanId)
	b, ok := v.([]byte)
	return b, ok && len(b) > 0
}

// datasetError is a dataset AddNetwork refuses; the reason names the
// sub-TLV, never a value.
func datasetError(format string, args ...any) error {
	return fmt.Errorf("netinfra: operational dataset: "+format, args...)
}

// validDatasetFraming is chip's OperationalDatasetView::IsValid
// (src/lib/support/ThreadOperationalDataset.cpp:151-164): at most 254
// bytes, every TLV inside the blob, no TLV in the extended-length form.
func validDatasetFraming(blob []byte) bool {
	if len(blob) > maxDatasetLen {
		return false
	}
	for i := 0; i < len(blob); {
		if i+2 > len(blob) || blob[i+1] == 0xff {
			return false
		}
		i += 2 + int(blob[i+1])
		if i > len(blob) {
			return false
		}
	}
	return true
}

// parseDataset holds an AddNetwork dataset to chip's checks
// (ThreadNetworkDirectoryCluster.cpp:231-245): it frames as a dataset and
// carries the Active Timestamp, Channel, Channel Mask, Extended PAN ID,
// Network Key, Mesh-Local Prefix, Network Name, PAN ID, PKSc and Security
// Policy sub-TLVs in the sizes chip's getters demand
// (ThreadOperationalDataset.cpp:193-301). The TLVs are decoded by the
// commissioner's codec (controller/thread DecodeDataset, which wave 26a of
// concept v10 moves to thread/); chip's stricter sizes — the three-byte
// Channel on page 0 (:202-211), a PSKc of 16 bytes (:276-283), a network
// name of at most 16 (:257-265), a non-empty Channel Mask (:285-292) — are
// checked on top.
func parseDataset(blob []byte) (directoryEntry, error) {
	if !validDatasetFraming(blob) {
		return directoryEntry{}, datasetError("not a framed dataset of at most %d bytes", maxDatasetLen)
	}
	ds, err := thread.DecodeDataset(blob)
	if err != nil {
		return directoryEntry{}, datasetError("%w", err)
	}
	switch {
	case ds.ExtPANID == nil:
		return directoryEntry{}, datasetError("no Extended PAN ID")
	case ds.ActiveTimestamp == nil:
		return directoryEntry{}, datasetError("no Active Timestamp")
	case ds.Channel == nil:
		return directoryEntry{}, datasetError("no Channel")
	case len(ds.ChannelMask) == 0:
		return directoryEntry{}, datasetError("no Channel Mask")
	case ds.NetworkKey == nil:
		return directoryEntry{}, datasetError("no Network Key")
	case ds.MeshLocalPrefix == nil:
		return directoryEntry{}, datasetError("no Mesh-Local Prefix")
	case ds.NetworkName == nil || len(*ds.NetworkName) > maxNetworkNameLen:
		return directoryEntry{}, datasetError("no Network Name of at most %d bytes", maxNetworkNameLen)
	case ds.PANID == nil:
		return directoryEntry{}, datasetError("no PAN ID")
	case len(ds.PSKc) != pskcLen:
		return directoryEntry{}, datasetError("no PSKc of %d bytes", pskcLen)
	case ds.SecurityPolicy == nil:
		return directoryEntry{}, datasetError("no Security Policy")
	}
	entries, err := thread.WalkTLVs(blob)
	if err != nil {
		return directoryEntry{}, datasetError("%w", err)
	}
	for _, e := range entries {
		if e.Type == thread.TLVChannel {
			if len(e.Value) != 3 || e.Value[0] != 0 {
				return directoryEntry{}, datasetError("Channel is not the three-byte form on page 0")
			}
			break
		}
	}
	return directoryEntry{
		dataset:   slices.Clone(blob),
		timestamp: binary.BigEndian.Uint64(ds.ActiveTimestamp),
		network: ThreadNetwork{
			ExtendedPanId:   slices.Clone(ds.ExtPANID),
			NetworkName:     *ds.NetworkName,
			Channel:         *ds.Channel,
			ActiveTimestamp: binary.BigEndian.Uint64(ds.ActiveTimestamp),
		},
	}, nil
}

// addNetwork is HandleAddNetworkRequest (:213-276): a dataset chip's
// checks refuse is CONSTRAINT_ERROR (:234-245), one whose Active Timestamp
// is not newer than the stored entry's INVALID_IN_STATE (:249-266), a new
// network beyond the capacity RESOURCE_EXHAUSTED (:275), a store's error
// FAILURE; a stored one reports ThreadNetworks (:270).
func (s *ThreadDirectoryServer) addNetwork(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[tdir.AddNetworkRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "netinfra: AddNetwork payload")
	}
	e, err := parseDataset(req.OperationalDataset)
	if err != nil {
		s.logger.Info("netinfra.thread_directory.add_network.refused", slog.String("reason", err.Error()))
		return nil, spec.Errorf(im.StatusConstraintError, "%w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := string(e.network.ExtendedPanId)
	old, exists := s.entries[id]
	if exists && e.timestamp <= old.timestamp {
		return nil, spec.Errorf(im.StatusInvalidInState, "netinfra: AddNetwork: the Active Timestamp is not newer than the stored network's")
	}
	if !exists && len(s.order) >= int(s.store.Capacity()) {
		s.logger.Info("netinfra.thread_directory.add_network.refused", slog.String("reason", "table full"))
		return nil, spec.Errorf(im.StatusResourceExhausted, "netinfra: AddNetwork: the Thread network table is full")
	}
	if err := s.store.SaveNetwork(ctx, e.network.ExtendedPanId, e.dataset); err != nil {
		s.logger.Error("netinfra.thread_directory.add_network.store", slog.Any("err", err))
		if errors.Is(err, ErrStoreFull) {
			return nil, spec.Errorf(im.StatusResourceExhausted, "netinfra: AddNetwork: %w", err)
		}
		return nil, spec.Errorf(im.StatusFailure, "netinfra: AddNetwork: %w", err)
	}
	s.entries[id] = e
	if !exists {
		s.order = append(s.order, id)
	}
	return nil, s.srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{tdir.AttrThreadNetworks: s.networksLocked()})
}

// removeNetwork is HandleRemoveNetworkRequest (:278-307): an Extended PAN
// ID of the wrong size is CONSTRAINT_ERROR (:283-286), the preferred
// network CONSTRAINT_ERROR (:293-297), one not stored NOT_FOUND (:306), a
// store's error FAILURE; a removal reports ThreadNetworks (:301).
func (s *ThreadDirectoryServer) removeNetwork(ctx context.Context, fields any) (any, error) {
	req, ok := spec.RequestOf[tdir.RemoveNetworkRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "netinfra: RemoveNetwork payload")
	}
	if len(req.ExtendedPanId) != ExtendedPanIDLen {
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: RemoveNetwork: Extended PAN ID of %d bytes", len(req.ExtendedPanId))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.preferredLocked(); ok && bytes.Equal(p, req.ExtendedPanId) {
		s.logger.Info("netinfra.thread_directory.remove_network.refused", slog.String("reason", "preferred network"))
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: RemoveNetwork: the preferred network cannot be removed")
	}
	id := string(req.ExtendedPanId)
	if _, ok := s.entries[id]; !ok {
		return nil, spec.Errorf(im.StatusNotFound, "netinfra: RemoveNetwork: no network of Extended PAN ID %X", req.ExtendedPanId)
	}
	if err := s.store.RemoveNetwork(ctx, req.ExtendedPanId); err != nil {
		s.logger.Error("netinfra.thread_directory.remove_network.store", slog.Any("err", err))
		return nil, spec.Errorf(im.StatusFailure, "netinfra: RemoveNetwork: %w", err)
	}
	delete(s.entries, id)
	s.order = slices.DeleteFunc(s.order, func(o string) bool { return o == id })
	return nil, s.srv.SetAttributesFrom(contract.OriginCommand, map[uint32]any{tdir.AttrThreadNetworks: s.networksLocked()})
}

// getOperationalDataset is HandleOperationalDatasetRequest (:309-335): a
// session that is not CASE is UNSUPPORTED_ACCESS (:315-318), an Extended
// PAN ID of the wrong size CONSTRAINT_ERROR (:320-323), one not stored
// NOT_FOUND (:334); the response carries the stored dataset as it was
// added (:328-330).
func (s *ThreadDirectoryServer) getOperationalDataset(ctx context.Context, fields any) (any, error) {
	if !isCASE(ctx) {
		return nil, spec.Errorf(im.StatusUnsupportedAccess, "netinfra: GetOperationalDataset needs a CASE session")
	}
	req, ok := spec.RequestOf[tdir.GetOperationalDatasetRequest](fields)
	if !ok {
		return nil, spec.Errorf(im.StatusInvalidCommand, "netinfra: GetOperationalDataset payload")
	}
	if len(req.ExtendedPanId) != ExtendedPanIDLen {
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: GetOperationalDataset: Extended PAN ID of %d bytes", len(req.ExtendedPanId))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[string(req.ExtendedPanId)]
	if !ok {
		return nil, spec.Errorf(im.StatusNotFound, "netinfra: GetOperationalDataset: no network of Extended PAN ID %X", req.ExtendedPanId)
	}
	return tdir.OperationalDatasetResponse{OperationalDataset: slices.Clone(e.dataset)}, nil
}

// checkPreferredLocked is WritePreferredExtendedPanId's rule (:139-151): a
// zero-length value is null (:145-146), any other must be an Extended PAN
// ID of a stored network, CONSTRAINT_ERROR otherwise (:148-149). It
// returns the value to store.
func (s *ThreadDirectoryServer) checkPreferredLocked(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: PreferredExtendedPanID is no octet string")
	}
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) != ExtendedPanIDLen {
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: PreferredExtendedPanID of %d bytes", len(b))
	}
	if _, ok := s.entries[string(b)]; !ok {
		return nil, spec.Errorf(im.StatusConstraintError, "netinfra: PreferredExtendedPanID %X names no stored network", b)
	}
	return slices.Clone(b), nil
}

// SetPreferredExtendedPanID sets the preferred network from the host,
// nil for none, under the write's rule.
func (s *ThreadDirectoryServer) SetPreferredExtendedPanID(extendedPanID []byte) error {
	return s.SetAttributes(map[uint32]any{tdir.AttrPreferredExtendedPanId: extendedPanID})
}

// SetAttributes implements [spec.AttributeSetter]: PreferredExtendedPanID
// under the write's rule; ThreadNetworks and ThreadNetworkTableSize are
// the store's ([spec.ErrNotHeld] for a value other than the one served).
func (s *ThreadDirectoryServer) SetAttributes(values map[uint32]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[uint32]any{}
	for _, id := range slices.Sorted(maps.Keys(values)) {
		switch v := values[id]; id {
		case tdir.AttrPreferredExtendedPanId:
			stored, err := s.checkPreferredLocked(v)
			if err != nil {
				return err
			}
			set[id] = stored
		case tdir.AttrThreadNetworks:
			if next, ok := v.(spec.List[ThreadNetwork]); ok {
				cur, _ := s.srv.Value(id)
				if sameNetworks(cur, next) {
					continue
				}
			}
			return fmt.Errorf("%w: netinfra: ThreadNetworks follows the store (ReloadNetworks)", spec.ErrNotHeld)
		case tdir.AttrThreadNetworkTableSize:
			if c, ok := v.(uint8); ok && c == s.store.Capacity() {
				continue
			}
			return fmt.Errorf("%w: netinfra: ThreadNetworkTableSize is the store's capacity", spec.ErrNotHeld)
		default:
			set[id] = v
		}
	}
	if len(set) == 0 {
		return nil
	}
	return s.srv.SetAttributes(set)
}

// --- plumbing -------------------------------------------------------------

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *ThreadDirectoryServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnAttributeChange implements [contract.AttributeChangeNotifier].
func (s *ThreadDirectoryServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.srv.OnAttributeChange(cb)
}

// MatterRead implements [contract.ClusterServer].
func (s *ThreadDirectoryServer) MatterRead(attrID uint32) (any, bool) {
	return s.srv.MatterRead(attrID)
}

// MatterWrite implements [contract.ClusterServer]: PreferredExtendedPanID
// under chip's rule (:139-151), then the definition's checks and the
// host's veto; every other attribute is read-only (:97-106).
func (s *ThreadDirectoryServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if attrID != tdir.AttrPreferredExtendedPanId {
		return s.srv.MatterWrite(ctx, attrID, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.checkPreferredLocked(value)
	if err != nil {
		return err
	}
	return s.srv.MatterWrite(ctx, attrID, stored)
}

// MatterInvoke implements [contract.ClusterServer]: a host's handler
// first ([spec.SelfHeld.Handle]), then the server's own.
func (s *ThreadDirectoryServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return spec.InvokeHeld(ctx, &s.SelfHeld, cmdID, fields)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *ThreadDirectoryServer) MatterAcceptedCommands() []uint32 {
	return s.srv.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (s *ThreadDirectoryServer) MatterGeneratedCommands() []uint32 {
	return s.srv.MatterGeneratedCommands()
}
