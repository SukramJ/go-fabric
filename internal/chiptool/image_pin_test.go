// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package chiptool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// imageDigest is the shape of an image reference pinned by digest.
var imageDigest = regexp.MustCompile(`^\S+@sha256:[0-9a-f]{64}$`)

// makefileImagePin reads `<name> ?= <image>` out of the Makefile and
// refuses a value that is not pinned by digest. The Makefile's rule over
// CHIP_BASE_IMAGE and CHIP_TEST_IMAGE ("Both pinned by digest, never a
// floating tag") was a comment the readers did not hold (review II,
// R2-P5-sup-11): a tag there would have reached the suite, the control
// leg and every family run unchecked.
func makefileImagePin(makefile []byte, name string) (string, error) {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `\s*\?=\s*(\S+)`).FindSubmatch(makefile)
	if m == nil {
		return "", fmt.Errorf("no %s pin in the Makefile", name)
	}
	img := string(m[1])
	if !imageDigest.MatchString(img) {
		return "", fmt.Errorf("the Makefile's %s (%s) is not pinned by digest (<image>@sha256:<64 hex>)", name, img)
	}
	return img, nil
}

// TestMakefileImagesPinnedByDigest holds the two harness image pins of
// the Makefile to a digest, without the chiptool tag, so a tag in their
// place fails every pull request rather than only a harness run.
func TestMakefileImagesPinnedByDigest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CHIP_TEST_IMAGE", "CHIP_BASE_IMAGE"} {
		if _, err := makefileImagePin(data, name); err != nil {
			t.Error(err)
		}
	}
	for src, ok := range map[string]bool{
		"CHIP_TEST_IMAGE ?= ghcr.io/thingfabric/go-fabric-chip@sha256:d683bf5bbca6b0d9a37bcce64a08715a65ebfe0e5c9ae8dc2e322ca8f4e006ff\n": true,
		"CHIP_TEST_IMAGE ?= ghcr.io/thingfabric/go-fabric-chip:6170af84\n":                                                                false,
		"CHIP_TEST_IMAGE ?= ghcr.io/thingfabric/go-fabric-chip@sha256:d683bf5b\n":                                                         false,
		"CHIP_TEST_IMAGE ?= ghcr.io/thingfabric/go-fabric-chip:latest@sha256:\n":                                                          false,
		"# CHIP_TEST_IMAGE ?= x@sha256:d683bf5bbca6b0d9a37bcce64a08715a65ebfe0e5c9ae8dc2e322ca8f4e006ff\n":                            false,
	} {
		if _, err := makefileImagePin([]byte(src), "CHIP_TEST_IMAGE"); (err == nil) != ok {
			t.Errorf("%q: accepted %v, want %v (%v)", src, err == nil, ok, err)
		}
	}
}
