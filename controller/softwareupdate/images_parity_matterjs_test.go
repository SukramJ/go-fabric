// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// The image-store cases of matter.js's DclOtaUpdateServiceTest
// (packages/protocol/test/dcl/DclOtaUpdateServiceTest.ts at 862ad75f) that
// the Images store has a counterpart for: the stored images
// checkForUpdate surfaces by isProduction, find, delete by filename, and
// the versions and modes it keeps side by side. An image matter.js
// downloads from a ledger is stored here in the mode its source names.

func image(t *testing.T, vid, pid uint16, version uint32) []byte {
	t.Helper()
	b, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: vid, ProductID: pid, SoftwareVersion: version, SoftwareVersionString: "v" + string(rune('0'+version)) + ".0.0",
		Payload: bytes.Repeat([]byte{0x5A}, 512),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func store(t *testing.T, imgs *softwareupdate.Images, vid, pid uint16, version, minApplicable uint32, mode softwareupdate.Mode) {
	t.Helper()
	info := &softwareupdate.UpdateInfo{VendorID: vid, ProductID: pid, SoftwareVersion: version, MinApplicable: minApplicable, MaxApplicable: version - 1}
	if _, err := imgs.Store(context.Background(), image(t, vid, pid, version), mode, info); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestImagesStoredUpdatesParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	check := func(imgs *softwareupdate.Images, production bool) (softwareupdate.UpdateInfo, bool) {
		return imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, softwareupdate.CheckOptions{IncludeStored: true, IsProduction: &production})
	}

	// :420-469
	t.Run("skips stored test-mode files when isProduction is true", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeTest)
		if u, ok := check(imgs, true); ok {
			t.Fatalf("a production check found %+v", u)
		}
		if u, ok := check(imgs, false); !ok || u.Source != softwareupdate.SourceTest {
			t.Fatalf("a test check: %+v %v", u, ok)
		}
	})

	// :471-513
	t.Run("surfaces local-mode stored files regardless of isProduction", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeLocal)
		if u, ok := check(imgs, true); !ok || u.Source != softwareupdate.SourceLocal {
			t.Fatalf("%+v %v", u, ok)
		}
	})

	// :515-554
	t.Run("surfaces stored prod-mode files when isProduction is false", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeProd)
		if u, ok := check(imgs, false); !ok || u.Source != softwareupdate.SourceProd || u.SoftwareVersion != 3 {
			t.Fatalf("%+v %v", u, ok)
		}
	})
}

func TestImagesFindDeleteParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// :939-1018
	t.Run("lists all stored OTA images", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeProd)
		store(t, imgs, 0xfff1, 0x8001, 2, 1, softwareupdate.ModeProd)
		store(t, imgs, 0xfff2, 0x8000, 1, 0, softwareupdate.ModeTest)
		if n := len(imgs.Find(ctx, softwareupdate.FindOptions{})); n != 3 {
			t.Fatalf("all: %d", n)
		}
		if n := len(imgs.Find(ctx, softwareupdate.FindOptions{VendorID: ptr[uint16](0xfff1)})); n != 2 {
			t.Fatalf("vendor: %d", n)
		}
		if f := imgs.Find(ctx, softwareupdate.FindOptions{VendorID: ptr[uint16](0xfff1), ProductID: ptr[uint16](0x8000)}); len(f) != 1 || f[0].ProductID != 0x8000 {
			t.Fatalf("product: %+v", f)
		}
		if f := imgs.Find(ctx, softwareupdate.FindOptions{IsProduction: ptr(false)}); len(f) != 1 || f[0].VendorID != 0xfff2 {
			t.Fatalf("test mode: %+v", f)
		}
	})

	// :1020-1025
	t.Run("returns empty array when no updates are stored", func(t *testing.T) {
		t.Parallel()
		if f := softwareupdate.NewImages(softwareupdate.ImagesConfig{}).Find(ctx, softwareupdate.FindOptions{}); len(f) != 0 {
			t.Fatalf("%+v", f)
		}
	})

	// :1027-1070
	t.Run("filters by mode using new mode parameter", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeProd)
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeTest)
		for _, m := range []softwareupdate.Mode{softwareupdate.ModeProd, softwareupdate.ModeTest} {
			if f := imgs.Find(ctx, softwareupdate.FindOptions{Mode: m}); len(f) != 1 || f[0].Mode != m {
				t.Fatalf("%s: %+v", m, f)
			}
		}
	})

	// :1157-1170
	t.Run("deletes specific OTA file by filename", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeProd)
		store(t, imgs, 0xfff1, 0x8001, 2, 1, softwareupdate.ModeProd)
		if err := imgs.Delete(ctx, "fff1.8000.prod.3"); err != nil {
			t.Fatal(err)
		}
		if f := imgs.Find(ctx, softwareupdate.FindOptions{}); len(f) != 1 || f[0].ProductID != 0x8001 {
			t.Fatalf("remaining %+v", f)
		}
	})

	// :1514-1560
	t.Run("stores multiple versions for same vid/pid/mode", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		store(t, imgs, 0xfff1, 0x8000, 3, 2, softwareupdate.ModeProd)
		store(t, imgs, 0xfff1, 0x8000, 4, 3, softwareupdate.ModeProd)
		f := imgs.Find(ctx, softwareupdate.FindOptions{VendorID: ptr[uint16](0xfff1), ProductID: ptr[uint16](0x8000)})
		versions := make([]uint32, 0, len(f))
		for _, s := range f {
			versions = append(versions, s.SoftwareVersion)
		}
		slices.Sort(versions)
		if !slices.Equal(versions, []uint32{3, 4}) {
			t.Fatalf("versions %v", versions)
		}
	})

	// :1562-1620
	t.Run("stores files with three different modes", func(t *testing.T) {
		t.Parallel()
		imgs := softwareupdate.NewImages(softwareupdate.ImagesConfig{})
		for _, m := range []softwareupdate.Mode{softwareupdate.ModeProd, softwareupdate.ModeTest, softwareupdate.ModeLocal} {
			store(t, imgs, 0xfff1, 0x8000, 3, 2, m)
		}
		f := imgs.Find(ctx, softwareupdate.FindOptions{VendorID: ptr[uint16](0xfff1), ProductID: ptr[uint16](0x8000)})
		modes := make([]string, 0, len(f))
		for _, s := range f {
			modes = append(modes, string(s.Mode))
		}
		slices.Sort(modes)
		if !slices.Equal(modes, []string{"local", "prod", "test"}) {
			t.Fatalf("modes %v", modes)
		}
	})
}
