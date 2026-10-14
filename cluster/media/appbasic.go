// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media

import (
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	basicdef "github.com/SukramJ/go-fabric/cluster/spec/applicationbasic"
)

// AppBasicState is the ApplicationBasic attribute state
// (application-basic.element.ts:20-44). The optional VendorName, VendorId
// and ProductId are served when the configuration lists them.
// Application and AllowedVendorList are mandatory and have no default: nil
// refuses construction ([ErrMissingState]).
type AppBasicState struct {
	VendorName         string
	VendorID           uint16
	ApplicationName    string
	ProductID          uint16
	Application        *basicdef.ApplicationStruct
	Status             basicdef.ApplicationStatusEnum
	ApplicationVersion string
	AllowedVendorList  []uint16
}

func (st AppBasicState) values() map[uint32]any {
	return map[uint32]any{
		basicdef.AttrVendorName:         st.VendorName,
		basicdef.AttrVendorId:           st.VendorID,
		basicdef.AttrApplicationName:    st.ApplicationName,
		basicdef.AttrProductId:          st.ProductID,
		basicdef.AttrApplication:        structOr(st.Application),
		basicdef.AttrStatus:             st.Status,
		basicdef.AttrApplicationVersion: st.ApplicationVersion,
		basicdef.AttrAllowedVendorList:  uint16sOr(st.AllowedVendorList),
	}
}

func (st AppBasicState) clone() AppBasicState {
	st.AllowedVendorList = slices.Clone(st.AllowedVendorList)
	return st
}

// AppBasicConfig carries the construction parameters of an
// ApplicationBasic server. The cluster has no commands: its state is the
// host's, set with SetState (the content app's Status as it starts, hides
// and stops).
type AppBasicConfig struct {
	// Attributes lists the optional attributes served: AttrVendorName,
	// AttrVendorId, AttrProductId.
	Attributes []uint32
	// Initial is the state the server starts with.
	Initial AppBasicState
	// DataVersion is an optional host-owned tracker.
	DataVersion dataVersion
}

// AppBasicServer is an ApplicationBasic (0x050D) server.
type AppBasicServer struct {
	*spec.Server
	stateful[AppBasicState]
}

// NewAppBasic builds an ApplicationBasic server.
func NewAppBasic(cfg AppBasicConfig) (*AppBasicServer, error) {
	srv, err := spec.NewServer(basicdef.Definition, spec.Options{Attributes: cfg.Attributes},
		spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("media: ApplicationBasic: %w", err)
	}
	s := &AppBasicServer{Server: srv}
	if err := s.init(srv, cfg.Initial, AppBasicState.values, AppBasicState.clone); err != nil {
		return nil, fmt.Errorf("media: ApplicationBasic: %w", err)
	}
	if err := checkState(srv); err != nil {
		return nil, err
	}
	return s, nil
}
