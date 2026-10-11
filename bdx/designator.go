// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bdx

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FileDesignator names the file a transfer moves. On the wire it is bytes;
// [FileDesignator.Text] is the name matter.js stores it under.
//
// Mirrors matter.js packages/protocol/src/bdx/FileDesignator.ts.
type FileDesignator []byte

// bdxURI is matter.js's BDX URI pattern (FileDesignator.ts:22):
// `bdx://<16 upper-case hex digits of the source node id>/<designator>`.
var bdxURI = regexp.MustCompile(`^bdx://([0-9A-F]{16})/(\S+)$`)

// ParseBdxURI splits a BDX URI into its file designator and the node id of
// the file's owner. Mirrors FileDesignator.fromBdxUri (FileDesignator.ts:21-30).
func ParseBdxURI(uri string) (FileDesignator, uint64, error) {
	parts := bdxURI.FindStringSubmatch(uri)
	if parts == nil {
		return nil, 0, fmt.Errorf("bdx: invalid OTA URI %q", uri)
	}
	node, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return nil, 0, fmt.Errorf("bdx: invalid OTA URI %q: %w", uri, err)
	}
	return FileDesignator(parts[2]), node, nil
}

// Text is the designator's textual form: the bytes as characters when every
// byte is one of A-Z a-z 0-9 . - _ /, else "0x" and the lower-case hex.
// Mirrors FileDesignator.ts:47-65.
func (fd FileDesignator) Text() string {
	for _, b := range fd {
		valid := (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
			b == '.' || b == '-' || b == '_' || b == '/'
		if !valid {
			return "0x" + hex.EncodeToString(fd)
		}
	}
	return string(fd)
}

// BdxURI renders the designator as a BDX URI owned by sourceNode
// (FileDesignator.ts:71-73; NodeId.strOf is the 16-digit hex form).
func (fd FileDesignator) BdxURI(sourceNode uint64) string {
	return fmt.Sprintf("bdx://%s/%s", strings.ToUpper(fmt.Sprintf("%016x", sourceNode)), fd.Text())
}
