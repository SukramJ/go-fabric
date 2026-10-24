// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/node"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// --dcl prod|testnet|off (default off) lets the daemon ask the CSA
// Distributed Compliance Ledger (controller/dcl, ADR 0020 stage 6) whether
// the image its --ota-provider offers is a version the ledger lists for the
// requestor's vendor and product, and whether the image matches the
// ledger's checksum. The answer is logged; the image is offered either way
// — the embedded test image is in no ledger. Without --ota-provider the
// flag does nothing.
//
// What leaves the host when it is on: for every QueryImage the provider
// answers with an image, one HTTPS request to the ledger
// (GET /dcl/model/versions/{vid}/{pid}/{softwareVersion}) naming the
// requestor's vendor id, product id and the offered software version. No
// device identity, no certificate and no subject key identifier is sent:
// the daemon attests no device. A commissioner that attests with a
// dcl.CertificateStore asks the ledger for the subject key identifiers of
// the PAAs, CD signers and CRL issuers of the devices it commissions — this
// daemon has no commissioner. Off, nothing contacts the ledger; the vendor
// names the daemon shows are embedded (eligibility/vendor_name.go).

// dclClient is the ledger --dcl names, nil for off.
func dclClient(mode string) (*node.DCLClient, error) {
	switch mode {
	case "", "off":
		return nil, nil
	case "prod":
		return node.NewDCLClient(node.DCLProductionURL), nil
	case "testnet":
		return node.NewDCLClient(node.DCLTestNetURL), nil
	}
	return nil, fmt.Errorf("--dcl %q, want prod, testnet or off", mode)
}

// ledgerCheckedImages is an OTA image source whose offers are looked up in
// the ledger and logged.
type ledgerCheckedImages struct {
	node.OTAImageSource
	versions *node.DCLOTAVersions
	// image is the bytes of the offered image, checked against the
	// ledger's checksum.
	image  func(file string) []byte
	logger *slog.Logger
}

// QueryImage implements node.OTAImageSource: the inner source's
// answer, its version looked up in the ledger.
func (s *ledgerCheckedImages) QueryImage(ctx context.Context, peer node.OTAPeer, req provdef.QueryImageRequest) (img node.OTAImage, ok bool, err error) {
	img, ok, err = s.OTAImageSource.QueryImage(ctx, peer, req)
	if err != nil || !ok {
		return img, ok, err
	}
	attrs := []any{slog.Int("vid", int(req.VendorId)), slog.Int("pid", int(req.ProductId)), slog.Uint64("version", uint64(img.SoftwareVersion))}
	v, found, lerr := s.versions.Lookup(ctx, req.VendorId, req.ProductId, img.SoftwareVersion)
	switch {
	case lerr != nil:
		s.logger.Warn("dcl.ota.lookup_failed", append(attrs, slog.Any("err", lerr))...)
	case !found:
		s.logger.Warn("dcl.ota.not_in_ledger", attrs...)
	case v.OTAChecksum == "":
		s.logger.Info("dcl.ota.in_ledger", append(attrs, slog.Bool("checksum", false))...)
	default:
		if _, verr := node.VerifyOTAImage(bytes.NewReader(s.image(string(img.FileDesignator))), v, s.logger); verr != nil {
			s.logger.Warn("dcl.ota.ledger_mismatch", append(attrs, slog.Any("err", verr))...)
		} else {
			s.logger.Info("dcl.ota.in_ledger", append(attrs, slog.Bool("checksum", true))...)
		}
	}
	return img, ok, nil
}
