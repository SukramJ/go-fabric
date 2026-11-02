// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/controller/dcl"
)

// The image store: the storage half of matter.js's DclOtaUpdateService
// (packages/protocol/src/dcl/DclOtaUpdateService.ts at 862ad75f) over the
// ledger lookups of controller/dcl (dcl.OTAVersions) — stored images by
// vendor, product, mode and version, the update check over stored images
// and the ledgers, and the download of a ledger's image into the store.

// Mode is the storage mode of an image — OtaStorageMode
// (DclOtaUpdateService.ts:41): "prod" and "test" images came from the
// production and test-net ledgers, "local" ones from the host.
type Mode string

// The modes.
const (
	ModeProd  Mode = "prod"
	ModeTest  Mode = "test"
	ModeLocal Mode = "local"
)

// Sources of an update — OtaUpdateSource (DclOtaUpdateService.ts:39).
const (
	SourceLocal = "local"
	SourceProd  = dcl.SourceProduction
	SourceTest  = dcl.SourceTest
)

// ErrOTA is an update check, store or download that failed — matter.js
// OtaUpdateError (DclOtaUpdateService.ts:33-37).
var ErrOTA = errors.New("softwareupdate: OTA update failed")

// StoredImage is a stored image's entry — DclOtaUpdateService.OtaUpdateListEntry
// (:1298-1310).
type StoredImage struct {
	// Filename is "<vendor hex>.<product hex>.<mode>.<version>" (:534-536).
	Filename              string
	VendorID              uint16
	ProductID             uint16
	SoftwareVersion       uint32
	SoftwareVersionString string
	// MinApplicable and MaxApplicable are the header's range; nil when it
	// states none.
	MinApplicable, MaxApplicable *uint32
	Mode                         Mode
	Size                         int
	// SpecificationVersion is the one recorded at store time, 0 when
	// unknown.
	SpecificationVersion uint32
}

// FindOptions filter [Images.Find] — DclOtaUpdateService.FindOptions
// (:1312-1318). A nil field matches anything.
type FindOptions struct {
	VendorID, ProductID *uint16
	// IsProduction, without Mode, keeps prod images (true) or the others
	// (false) (:1094-1097).
	IsProduction *bool
	Mode         Mode
	// CurrentVersion keeps the images whose applicable range holds it
	// (:1117-1153).
	CurrentVersion *uint32
}

// UpdateInfo is an applicable update — matter.js
// DeviceSoftwareVersionModelDclSchemaWithSource (:43-46): a ledger record
// or a stored image, with the source it came from.
type UpdateInfo struct {
	VendorID, ProductID          uint16
	SoftwareVersion              uint32
	SoftwareVersionString        string
	MinApplicable, MaxApplicable uint32
	ReleaseNotesURL              string
	SpecificationVersion         uint32
	// Source is SourceLocal, SourceProd or SourceTest.
	Source string
	// ledger is the record of a ledger update, nil for a stored one.
	ledger *dcl.SoftwareVersion
}

// Blobs keeps the image files. [MemoryBlobs] is the default.
type Blobs interface {
	Put(ctx context.Context, name string, data []byte) error
	// Get returns fs.ErrNotExist for a missing file.
	Get(ctx context.Context, name string) ([]byte, error)
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]string, error)
}

// MemoryBlobs keeps the files in memory. Safe for concurrent use.
type MemoryBlobs struct {
	mu    sync.Mutex
	files map[string][]byte
}

// Put implements [Blobs].
func (m *MemoryBlobs) Put(_ context.Context, name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string][]byte{}
	}
	m.files[name] = slices.Clone(data)
	return nil
}

// Get implements [Blobs].
func (m *MemoryBlobs) Get(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return b, nil
}

// Delete implements [Blobs].
func (m *MemoryBlobs) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, name)
	return nil
}

// List implements [Blobs].
func (m *MemoryBlobs) List(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.files))
	for k := range m.files {
		out = append(out, k)
	}
	slices.Sort(out)
	return out, nil
}

// filenamePattern is OTA_FILENAME_REGEX (DclOtaUpdateService.ts:55),
// case-insensitive as its `i` flag makes it (W4-ctlint-08).
var filenamePattern = regexp.MustCompile(`(?i)^[0-9a-f]+[./][0-9a-f]+[./](?:prod|test|local)[./]\d+$`)

// metaSuffix names an image's recorded specification version (the
// "meta" context of :61-66).
const metaSuffix = ".meta"

// Images is the image store over Blobs and the ledgers. Safe for
// concurrent use.
type Images struct {
	blobs   Blobs
	ledger  *dcl.OTAVersions
	offline bool
	log     *slog.Logger
	// download fetches a ledger image; nil is the ledger's Download.
	download func(ctx context.Context, v dcl.SoftwareVersion) ([]byte, error)
}

// ImagesConfig configures [NewImages].
type ImagesConfig struct {
	// Blobs keeps the files; nil is a [MemoryBlobs].
	Blobs Blobs
	// Ledger asks the production and test-net ledgers; nil asks none, as
	// the service's offline option (:1283-1296): only stored images are
	// found, and nothing is downloaded.
	Ledger *dcl.OTAVersions
	// Logger receives the store's lines; nil is slog.Default().
	Logger *slog.Logger
}

// NewImages returns the image store.
func NewImages(cfg ImagesConfig) *Images {
	b := cfg.Blobs
	if b == nil {
		b = &MemoryBlobs{}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Images{blobs: b, ledger: cfg.Ledger, offline: cfg.Ledger == nil, log: log}
}

// fileName is #fileName (:534-536).
func fileName(vid, pid uint16, mode Mode, version uint32) string {
	return strconv.FormatUint(uint64(vid), 16) + "." + strconv.FormatUint(uint64(pid), 16) + "." + string(mode) + "." + strconv.FormatUint(uint64(version), 10)
}

// Store stores an image — DclOtaUpdateService.store (:541-612): it is
// written under its vendor, product, mode and version, read back and
// checked against its header (and against info's vendor, product and
// version when given), the specification version recorded; a failed check
// removes it.
func (i *Images) Store(ctx context.Context, image []byte, mode Mode, info *UpdateInfo) (StoredImage, error) {
	h, err := otaimage.ValidateFile(bytes.NewReader(image), storeValidation(info))
	if err != nil {
		return StoredImage{}, fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if info != nil {
		if err := verifyHeader(h, *info); err != nil {
			return StoredImage{}, err
		}
	}
	name := fileName(h.VendorID, h.ProductID, mode, h.SoftwareVersion)
	if err := i.blobs.Put(ctx, name, image); err != nil {
		return StoredImage{}, fmt.Errorf("%w: store %s: %w", ErrOTA, name, err)
	}
	if info != nil && info.SpecificationVersion != 0 {
		_ = i.blobs.Put(ctx, name+metaSuffix, []byte(strconv.FormatUint(uint64(info.SpecificationVersion), 10)))
	} else {
		_ = i.blobs.Delete(ctx, name+metaSuffix)
	}
	i.log.Debug("ota.images.stored", slog.String("file", name))
	st, _ := i.entry(ctx, name)
	return st, nil
}

// storeValidation is how Store verifies the whole file before it is kept:
// the ledger's checksum and file size when the record carries them, as
// matter.js #verifyUpdate passes them to OtaImageReader.file
// (DclOtaUpdateService.ts:388-408, :573-574; OtaImageReader.ts:65-110);
// otherwise the payload size and the header's payload digest
// (OtaImageReader.ts:261-278), so a truncated or corrupted local image is
// refused before it is offered over BDX (W4-ctlint-03).
func storeValidation(info *UpdateInfo) otaimage.FileOptions {
	opts := otaimage.FileOptions{SkipFullChecksum: true}
	if info == nil || info.ledger == nil {
		return opts
	}
	l := info.ledger
	if l.OTAFileSize > 0 {
		size := l.OTAFileSize
		opts.ExpectedTotalSize = &size
	}
	if l.OTAChecksum != "" {
		opts.SkipFullChecksum = false
		opts.ExpectedChecksum = l.OTAChecksum
		opts.ChecksumType = l.OTAChecksumType
	}
	return opts
}

// verifyHeader is #verifyUpdate's header check (:388-431).
func verifyHeader(h otaimage.Header, info UpdateInfo) error {
	switch {
	case h.VendorID != info.VendorID:
		return fmt.Errorf("%w: OTA image vendor ID mismatch: expected %d, got %d", ErrOTA, info.VendorID, h.VendorID)
	case h.ProductID != info.ProductID:
		return fmt.Errorf("%w: OTA image product ID mismatch: expected %d, got %d", ErrOTA, info.ProductID, h.ProductID)
	case h.SoftwareVersion != info.SoftwareVersion:
		return fmt.Errorf("%w: OTA image software version mismatch: expected %d, got %d", ErrOTA, info.SoftwareVersion, h.SoftwareVersion)
	}
	return nil
}

// entry reads a stored image's entry from its header (#checkEntry,
// :1117-1153).
func (i *Images) entry(ctx context.Context, name string) (StoredImage, bool) {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return StoredImage{}, false
	}
	b, err := i.blobs.Get(ctx, name)
	if err != nil {
		return StoredImage{}, false
	}
	h, _, _, err := otaimage.ReadHeader(bytes.NewReader(b))
	if err != nil {
		i.log.Warn("ota.images.unreadable", slog.String("file", name), slog.Any("err", err))
		return StoredImage{}, false
	}
	st := StoredImage{
		Filename: name, VendorID: h.VendorID, ProductID: h.ProductID, SoftwareVersion: h.SoftwareVersion,
		SoftwareVersionString: h.SoftwareVersionString, MinApplicable: h.MinApplicableSoftwareVersion,
		// A host-chosen "FFF1.8000.PROD.3" is the prod image: the pattern
		// admits any case (:55), the Mode constants are lower case.
		MaxApplicable: h.MaxApplicableSoftwareVersion, Mode: Mode(strings.ToLower(parts[2])), Size: len(b),
	}
	if m, err := i.blobs.Get(ctx, name+metaSuffix); err == nil {
		if v, err := strconv.ParseUint(string(m), 10, 32); err == nil && v > 0 {
			st.SpecificationVersion = uint32(v)
		}
	}
	return st, true
}

// Find lists the stored images opts selects, by mode (prod, test, local),
// vendor, product and version — DclOtaUpdateService.find (:996-1153).
func (i *Images) Find(ctx context.Context, opts FindOptions) []StoredImage {
	names, err := i.blobs.List(ctx)
	if err != nil {
		return nil
	}
	var out []StoredImage
	for _, name := range names {
		if strings.HasSuffix(name, metaSuffix) || !filenamePattern.MatchString(name) {
			continue
		}
		st, ok := i.entry(ctx, name)
		if !ok {
			continue
		}
		switch {
		case opts.VendorID != nil && st.VendorID != *opts.VendorID,
			opts.ProductID != nil && st.ProductID != *opts.ProductID,
			opts.Mode != "" && st.Mode != opts.Mode,
			opts.Mode == "" && opts.IsProduction != nil && *opts.IsProduction && st.Mode != ModeProd,
			opts.Mode == "" && opts.IsProduction != nil && !*opts.IsProduction && st.Mode == ModeProd:
			continue
		}
		if c := opts.CurrentVersion; c != nil {
			if (st.MinApplicable != nil && *c < *st.MinApplicable) || (st.MaxApplicable != nil && *c > *st.MaxApplicable) {
				continue
			}
		}
		out = append(out, st)
	}
	order := map[Mode]int{ModeProd: 0, ModeTest: 1, ModeLocal: 2}
	slices.SortStableFunc(out, func(a, b StoredImage) int {
		return cmp.Or(cmp.Compare(order[a.Mode], order[b.Mode]), cmp.Compare(a.VendorID, b.VendorID),
			cmp.Compare(a.ProductID, b.ProductID), cmp.Compare(a.SoftwareVersion, b.SoftwareVersion))
	})
	return out
}

// Delete removes the stored image named filename and its metadata
// (DclOtaUpdateService.delete, :1187-1275, by filename).
func (i *Images) Delete(ctx context.Context, filename string) error {
	if !filenamePattern.MatchString(filename) {
		return fmt.Errorf("%w: invalid OTA filename format: %s", ErrOTA, filename)
	}
	_ = i.blobs.Delete(ctx, filename+metaSuffix)
	return i.blobs.Delete(ctx, filename)
}

// Open opens the stored image named filename for a BDX transfer
// (fileDesignatorForUpdate, :1156-1185).
func (i *Images) Open(ctx context.Context, filename string) (bdx.Blob, error) {
	if !filenamePattern.MatchString(filename) {
		return nil, fs.ErrNotExist
	}
	b, err := i.blobs.Get(ctx, filename)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// CheckOptions configure [Images.CheckForUpdate].
type CheckOptions struct {
	// IncludeStored considers the stored images beside the ledgers.
	IncludeStored bool
	// IsProduction, when set, asks the production ledger only (true) or
	// the test-net one only (false); nil asks both.
	IsProduction *bool
	// Target, when set, considers that version only.
	Target *uint32
}

// CheckForUpdate is the applicable update of vid/pid over current —
// checkForUpdate (:292-386): the stored images (highest first; a stored
// test image only when the test-net ledger is allowed), then the
// production and test-net ledgers; the highest version wins, or the
// target version alone.
func (i *Images) CheckForUpdate(ctx context.Context, vid, pid uint16, current uint32, opts CheckOptions) (UpdateInfo, bool) {
	var found []UpdateInfo
	if opts.IncludeStored {
		local := i.Find(ctx, FindOptions{VendorID: &vid, ProductID: &pid, CurrentVersion: &current})
		for k := len(local) - 1; k >= 0; k-- {
			e := local[k]
			if opts.IsProduction != nil && *opts.IsProduction && e.Mode == ModeTest {
				continue
			}
			u := UpdateInfo{
				VendorID: vid, ProductID: pid, SoftwareVersion: e.SoftwareVersion, SoftwareVersionString: e.SoftwareVersionString,
				MaxApplicable: e.SoftwareVersion - 1, SpecificationVersion: e.SpecificationVersion, Source: sourceOf(e.Mode),
			}
			if e.MinApplicable != nil {
				u.MinApplicable = *e.MinApplicable
			}
			if e.MaxApplicable != nil {
				u.MaxApplicable = *e.MaxApplicable
			}
			if u.SoftwareVersion > current && current >= u.MinApplicable && current <= u.MaxApplicable {
				if opts.Target != nil && u.SoftwareVersion == *opts.Target {
					return u, true
				}
				found = append(found, u)
			}
		}
	}
	if i.ledger != nil {
		ledger := *i.ledger
		if opts.IsProduction != nil {
			if *opts.IsProduction {
				ledger.TestClient = nil
			} else {
				ledger.Client = nil
			}
		}
		if u, ok := ledger.CheckForUpdate(ctx, vid, pid, current, opts.Target); ok {
			v := u.SoftwareVersion
			info := UpdateInfo{
				VendorID: v.VID, ProductID: v.PID, SoftwareVersion: v.SoftwareVersion, SoftwareVersionString: v.SoftwareVersionString,
				MinApplicable: v.MinApplicableSoftwareVersion, MaxApplicable: v.MaxApplicableSoftwareVersion,
				ReleaseNotesURL: v.ReleaseNotesURL, SpecificationVersion: v.SpecificationVersion, Source: u.Source, ledger: &v,
			}
			if opts.Target != nil {
				return info, true
			}
			found = append(found, info)
		}
	}
	if opts.Target != nil || len(found) == 0 {
		return UpdateInfo{}, false
	}
	slices.SortStableFunc(found, func(a, b UpdateInfo) int { return cmp.Compare(b.SoftwareVersion, a.SoftwareVersion) })
	return found[0], true
}

func sourceOf(m Mode) string {
	switch m {
	case ModeProd:
		return SourceProd
	case ModeTest:
		return SourceTest
	case ModeLocal:
	}
	return SourceLocal
}

func modeOf(source string) Mode {
	switch source {
	case SourceProd:
		return ModeProd
	case SourceTest:
		return ModeTest
	}
	return ModeLocal
}

// DownloadUpdate makes info's image available in the store and returns
// its filename — downloadUpdate (:615-720): a stored image that matches
// its header is used; one that does not is removed; then a ledger image is
// downloaded, verified and stored. A stored image needs no download, and
// offline (no ledger) nothing is downloaded.
func (i *Images) DownloadUpdate(ctx context.Context, info UpdateInfo) (string, error) {
	mode := modeOf(info.Source)
	name := fileName(info.VendorID, info.ProductID, mode, info.SoftwareVersion)
	if b, err := i.blobs.Get(ctx, name); err == nil {
		h, _, _, herr := otaimage.ReadHeader(bytes.NewReader(b))
		if herr == nil && verifyHeader(h, info) == nil {
			return name, nil
		}
		_ = i.Delete(ctx, name)
	}
	if i.offline || info.ledger == nil {
		return "", fmt.Errorf("%w: no valid OTA image %s (%s) is stored and none is downloaded", ErrOTA, name, info.SoftwareVersionString)
	}
	var buf bytes.Buffer
	if i.download != nil {
		b, err := i.download(ctx, *info.ledger)
		if err != nil {
			return "", err
		}
		buf.Write(b)
	} else if err := i.ledger.Download(ctx, *info.ledger, &buf); err != nil {
		return "", fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if _, err := dcl.VerifyImage(bytes.NewReader(buf.Bytes()), *info.ledger, i.log); err != nil {
		return "", fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if _, err := i.Store(ctx, buf.Bytes(), mode, &info); err != nil {
		return "", err
	}
	return name, nil
}
