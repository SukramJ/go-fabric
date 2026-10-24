// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	icddef "github.com/SukramJ/go-fabric/cluster/spec/icdmanagement"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// icdVerificationKeyField is the VerificationKey field of the two ICD
// Management requests that carry one.
var icdVerificationKeyField = map[uint32]uint32{
	icddef.CmdRegisterClient:   3,
	icddef.CmdUnregisterClient: 1,
}

// decodeICDRequest decodes RegisterClient and UnregisterClient through the
// generated definition with the VerificationKey's length bound dropped: a
// key of another length reaches the server, whose byte comparison refuses
// it with FAILURE, instead of failing the decode with CONSTRAINT_ERROR. Only
// a Manage caller's key is checked at all; an Administrator's is ignored
// (Core §9.16.7.1, §9.16.7.3). Mirrors matter.js
// packages/node/src/behaviors/icd-management/IcdManagementServer.ts:58-75
// (IcdManagementSchema, VerificationKey extended to constraint "none").
func decodeICDRequest(path im.ConcreteCommandPath, n spec.Node, decode func(spec.Node) (any, error)) (any, error) {
	field, ok := icdVerificationKeyField[path.Command]
	if !ok || n.El.Type != tlv.TypeStructure {
		return decode(n)
	}
	var odd *[]byte
	children := make([]spec.Node, 0, len(n.Children))
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == field && c.El.Type >= tlv.TypeOctetStr1 && c.El.Type <= tlv.TypeOctetStr8 && len(c.El.Octets) != 16 {
			k := slices.Clone(c.El.Octets)
			odd = &k
			continue
		}
		children = append(children, c)
	}
	if odd == nil {
		return decode(n)
	}
	n.Children = children
	fields, err := decode(n)
	if err != nil {
		return fields, err
	}
	switch req := fields.(type) {
	case icddef.RegisterClientRequest:
		req.VerificationKey = odd
		return req, nil
	case icddef.UnregisterClientRequest:
		req.VerificationKey = odd
		return req, nil
	}
	return fields, nil
}
