// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	// hostURL marks the record UpdateInfoFromURL builds for a URL the host
	// named: the one record that is not the ledger's.
	hostURL bool
}

// hostSupplied reports whether info is an image the host supplied
// itself: of [SourceLocal], with no record ([UpdateInfoFromImage], a
// stored local image) or the record of a URL the host named
// ([Images.UpdateInfoFromURL]). Every other update — any other Source,
// an unknown one included — is the ledger's and takes the ledger's rules.
func hostSupplied(info UpdateInfo) bool {
	return info.Source == SourceLocal && (info.ledger == nil || info.hostURL)
}

// Blobs keeps the image files. [MemoryBlobs] is the default.
type Blobs interface {
	Put(ctx context.Context, name string, data []byte) error
	// Get returns fs.ErrNotExist for a missing file.
	Get(ctx context.Context, name string) ([]byte, error)
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]string, error)
}

// BlobStreamer is the optional streaming side of a [Blobs]. When the
// store's Blobs implements it, [Images.DownloadUpdate] writes a ledger
// image into it as the image arrives and verifies it by reading it back,
// and every other read of an image ([Images.Find], [Images.Open], the
// check of a stored image) reads the file in place; without it each image
// passes through memory whole (Put, Get). An image's small records — its
// .meta and its provenance (.origin) — go through Put and Get either way.
//
// A backend opts in by implementing it on its own type; [MemoryBlobs]
// does not. Embedding a type that implements it promotes the capability:
// the embedding type's own Put and Get are then no longer used for image
// files.
type BlobStreamer interface {
	// CreateBlob starts writing the file name. Nothing of it is visible
	// under name before the writer's Commit, which replaces a file of the
	// same name; Abort discards what was written.
	CreateBlob(ctx context.Context, name string) (BlobWriter, error)
	// OpenBlob opens the file name for reading at any offset; a missing
	// file wraps fs.ErrNotExist. A blob that implements io.Closer is
	// closed after the store's own reads, and one [Images.Open] hands to a
	// BDX transfer is closed by the server when that transfer ends
	// ([bdx.ServeOptions].Open).
	OpenBlob(ctx context.Context, name string) (bdx.Blob, error)
}

// BlobWriter writes one file of a [BlobStreamer]. It is used by one
// goroutine.
type BlobWriter interface {
	io.Writer
	// Commit makes the written bytes the file's content.
	Commit() error
	// Abort discards the written bytes and leaves the file as it was; it
	// does nothing after Commit.
	Abort() error
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

// Get implements [Blobs]. The slice it returns is the stored file, not a
// copy (an image is not doubled per read): the caller only reads it — a
// write into it would change the file under every other reader (review
// II, R2-P6-conc-1916).
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

// originSuffix names a stored image's provenance record, written when the
// image is stored: "host" for an image the host supplied (Store without a
// ledger record, an image by URL), "ledger <type> <base64>" for a ledger
// image checked against the record's whole-file checksum, "ledger" for one
// stored without a checksum. A stored prod or test image whose record is
// "ledger", or that has none (stored before the store kept the record), is
// not offered, reused or opened unless
// [ImagesConfig].AllowUnverifiedLedgerImages is set; a local image needs
// no record.
const originSuffix = ".origin"

// Images is the image store over Blobs and the ledgers. Safe for
// concurrent use.
type Images struct {
	blobs   Blobs
	ledger  *dcl.OTAVersions
	offline bool
	log     *slog.Logger
	// allowUnverified is ImagesConfig.AllowUnverifiedLedgerImages.
	allowUnverified bool
	// download fetches a ledger image into w; nil is the ledger's
	// Download. A test seam.
	download func(ctx context.Context, v dcl.SoftwareVersion, w io.Writer) error

	// hiddenMu guards hidden: the names a streamed download has committed
	// and not yet verified, which Find and Open do not offer meanwhile.
	hiddenMu sync.Mutex
	hidden   map[string]int

	// names serialises the writers of one file name: DownloadUpdate and
	// Store hold a name's lock from their first look at the stored file to
	// their last write, so a second writer of that name neither reads a
	// file the first is replacing nor removes the one the first verified
	// (review II, R2-P6-conc-1901).
	names nameLocks
}

// nameLocks is a lock per file name; an unused name holds no entry.
type nameLocks struct {
	mu    sync.Mutex
	locks map[string]*nameLock
	// waited, when set, is called when a caller finds its name locked and
	// is about to wait. A test seam.
	waited func(name string)
}

type nameLock struct {
	held  sync.Mutex
	users int
}

// lock takes name's lock and returns its release.
func (n *nameLocks) lock(name string) func() {
	n.mu.Lock()
	if n.locks == nil {
		n.locks = map[string]*nameLock{}
	}
	l := n.locks[name]
	if l == nil {
		l = &nameLock{}
		n.locks[name] = l
	}
	l.users++
	waited := n.waited
	n.mu.Unlock()
	if !l.held.TryLock() {
		if waited != nil {
			waited(name)
		}
		l.held.Lock()
	}
	return func() {
		l.held.Unlock()
		n.mu.Lock()
		l.users--
		if l.users == 0 {
			delete(n.locks, name)
		}
		n.mu.Unlock()
	}
}

// ImagesConfig configures [NewImages].
type ImagesConfig struct {
	// Blobs keeps the files; nil is a [MemoryBlobs]. One that also
	// implements [BlobStreamer] receives a ledger image as it downloads.
	Blobs Blobs
	// Ledger asks the production and test-net ledgers; nil asks none, as
	// the service's offline option (:1283-1296): only stored images are
	// found, and nothing is downloaded. Its MaxImageSize bounds every
	// download of the store ([Images.DownloadUpdate],
	// [Images.UpdateInfoFromURL]) at [dcl.DefaultMaxImageSize] when it
	// sets none. DownloadUpdate holds the image in memory until it is
	// verified unless Blobs is a [BlobStreamer]; UpdateInfoFromURL reads
	// the header only.
	Ledger *dcl.OTAVersions
	// Logger receives the store's lines; nil is slog.Default().
	Logger *slog.Logger
	// AllowUnverifiedLedgerImages admits a ledger image (of the
	// production or the test-net ledger) that is not bound to the
	// ledger's whole-file checksum (OTAChecksum): an update whose record
	// states none, and a stored prod or test image the store does not
	// record as checked against one or as supplied by the host (one stored
	// from a record without a checksum, or by a release before the store
	// kept provenance records). Without it
	// [Images.DownloadUpdate] refuses such an update before it downloads
	// anything or reuses a stored image, with [ErrOTA] and a warning line
	// (dcl.ota.no_checksum), [Images.CheckForUpdate] does not offer such a
	// stored image and [Images.Open] does not open it; the file is kept.
	// With it each update so made available is logged
	// (dcl.ota.no_checksum_admitted). An image the host supplies itself
	// ([UpdateInfoFromImage], [Images.UpdateInfoFromURL], a stored local
	// image, [Images.Store] without a ledger record) needs no checksum
	// either way.
	AllowUnverifiedLedgerImages bool
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
	return &Images{
		blobs: b, ledger: cfg.Ledger, offline: cfg.Ledger == nil, log: log, allowUnverified: cfg.AllowUnverifiedLedgerImages,
		hidden: map[string]int{},
	}
}

// openBlob opens the stored file name: in place through the backend's
// [BlobStreamer], else its whole bytes. A missing file wraps
// fs.ErrNotExist.
func (i *Images) openBlob(ctx context.Context, name string) (bdx.Blob, error) {
	if s, ok := i.blobs.(BlobStreamer); ok {
		return s.OpenBlob(ctx, name)
	}
	b, err := i.blobs.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

// closeBlob closes a blob the store opened for its own reads.
func closeBlob(b bdx.Blob) {
	if c, ok := b.(io.Closer); ok {
		_ = c.Close()
	}
}

// blobReader reads b from its start.
func blobReader(b bdx.Blob) io.Reader { return io.NewSectionReader(b, 0, b.Size()) }

// hide keeps name out of Find and Open until the returned func runs.
func (i *Images) hide(name string) func() {
	i.hiddenMu.Lock()
	i.hidden[name]++
	i.hiddenMu.Unlock()
	return func() {
		i.hiddenMu.Lock()
		i.hidden[name]--
		if i.hidden[name] <= 0 {
			delete(i.hidden, name)
		}
		i.hiddenMu.Unlock()
	}
}

// isHidden reports whether a streamed download is verifying name.
func (i *Images) isHidden(name string) bool {
	i.hiddenMu.Lock()
	defer i.hiddenMu.Unlock()
	return i.hidden[name] > 0
}

// fetch downloads the image of info, which carries its record, into w:
// an image the host named by URL ([Images.UpdateInfoFromURL]) through
// [dcl.OTAVersions.DownloadHostURL], which admits http and any address;
// every other through [dcl.OTAVersions.Download], whose URL must be https
// and, without a host client, public — a test-net update with the
// ledger's TestClient as its client, so that the test-net client's
// HTTPClient and AllowPrivateDestinations apply, every other with Client.
func (i *Images) fetch(ctx context.Context, info UpdateInfo, w io.Writer) error {
	if i.download != nil {
		return i.download(ctx, *info.ledger, w)
	}
	if info.hostURL && hostSupplied(info) {
		return i.ledger.DownloadHostURL(ctx, *info.ledger, w)
	}
	ledger := i.ledger
	if info.Source == SourceTest {
		test := *i.ledger
		test.Client = i.ledger.TestClient
		ledger = &test
	}
	return ledger.Download(ctx, *info.ledger, w)
}

// originOf is the provenance record of an image stored for info
// (originSuffix).
func originOf(info *UpdateInfo) string {
	switch {
	case info == nil || info.ledger == nil || hostSupplied(*info):
		return "host"
	case info.ledger.OTAChecksum == "":
		return "ledger"
	}
	return "ledger " + strconv.FormatUint(uint64(info.ledger.OTAChecksumType), 10) + " " + info.ledger.OTAChecksum
}

// recordOrigin records the provenance of the stored image name, stored
// for info. A failed write is logged and the former record removed, so
// that the image counts as unbound to a ledger checksum. The record is
// also removed before an image is written (Store, downloadInto), and a
// removal that fails there fails the write. A record therefore names the
// bytes it was written for, unless the removal after a failed record
// write fails as well — a store failure, logged — or the files are
// changed outside the store.
func (i *Images) recordOrigin(ctx context.Context, name string, info *UpdateInfo) {
	if err := i.blobs.Put(ctx, name+originSuffix, []byte(originOf(info))); err != nil {
		i.log.Warn("ota.images.origin_not_recorded", slog.String("file", name), slog.Any("err", err))
		_ = i.forgetOrigin(ctx, name) // logged; the record is gone or the store is failing
	}
}

// forgetOrigin removes the provenance record of name. A failure other
// than a missing record is logged and returned: a writer that goes on
// would leave the former record on the new bytes (review II, 5b-dcl R9).
func (i *Images) forgetOrigin(ctx context.Context, name string) error {
	if err := i.blobs.Delete(ctx, name+originSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		i.log.Warn("ota.images.origin_not_removed", slog.String("file", name), slog.Any("err", err))
		return fmt.Errorf("%w: remove the provenance record of %s: %w", ErrOTA, name, err)
	}
	return nil
}

// vouched reports whether the stored image name needs no admission: a
// local image, or one whose provenance record names the host or a ledger
// checksum the image was checked against.
func (i *Images) vouched(ctx context.Context, name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) == 4 && Mode(strings.ToLower(parts[2])) == ModeLocal {
		return true
	}
	rec, err := i.blobs.Get(ctx, name+originSuffix)
	if err != nil {
		return false
	}
	if string(rec) == "host" {
		return true
	}
	f := strings.Fields(string(rec))
	if len(f) != 3 || f[0] != "ledger" {
		return false
	}
	_, perr := strconv.ParseUint(f[1], 10, 8)
	return perr == nil
}

// servable reports whether the stored image name may be offered and
// served: when it is vouched for, or the store admits ledger images
// unbound to a ledger checksum.
func (i *Images) servable(ctx context.Context, name string) bool {
	return i.allowUnverified || i.vouched(ctx, name)
}

// fileName is #fileName (:534-536).
func fileName(vid, pid uint16, mode Mode, version uint32) string {
	return strconv.FormatUint(uint64(vid), 16) + "." + strconv.FormatUint(uint64(pid), 16) + "." + string(mode) + "." + strconv.FormatUint(uint64(version), 10)
}

// Store stores an image — DclOtaUpdateService.store (:541-612): it is
// written under its vendor, product, mode and version, read back and
// checked against its header (and against info's vendor, product and
// version when given), the specification version recorded; a failed check
// removes it. Store and [Images.DownloadUpdate] of one file name run one
// at a time. A provenance record that cannot be removed before the image
// is written fails the call with [ErrOTA] and nothing is written (review
// II, 5b-dcl R9).
func (i *Images) Store(ctx context.Context, image []byte, mode Mode, info *UpdateInfo) (StoredImage, error) {
	h, err := checkImage(image, info)
	if err != nil {
		return StoredImage{}, err
	}
	name := fileName(h.VendorID, h.ProductID, mode, h.SoftwareVersion)
	defer i.names.lock(name)()
	return i.store(ctx, name, image, info)
}

// checkImage is Store's check of a whole image before it is kept: the
// file (storeValidation) and, when info is given, its header against
// info's vendor, product and version.
func checkImage(image []byte, info *UpdateInfo) (otaimage.Header, error) {
	h, err := otaimage.ValidateFile(bytes.NewReader(image), storeValidation(info))
	if err != nil {
		return otaimage.Header{}, fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if info != nil {
		if err := verifyHeader(h, *info); err != nil {
			return otaimage.Header{}, err
		}
	}
	return h, nil
}

// store is Store under name's lock, once the image is checked.
func (i *Images) store(ctx context.Context, name string, image []byte, info *UpdateInfo) (StoredImage, error) {
	if err := i.forgetOrigin(ctx, name); err != nil {
		return StoredImage{}, err
	}
	if err := i.blobs.Put(ctx, name, image); err != nil {
		return StoredImage{}, fmt.Errorf("%w: store %s: %w", ErrOTA, name, err)
	}
	var spec uint32
	if info != nil {
		spec = info.SpecificationVersion
	}
	i.recordMeta(ctx, name, spec)
	i.recordOrigin(ctx, name, info)
	i.log.Debug("ota.images.stored", slog.String("file", name))
	st, _ := i.entry(ctx, name)
	return st, nil
}

// storeValidation is how Store verifies the whole file before it is kept:
// the ledger's checksum and file size when the record carries them, as
// matter.js #verifyUpdate passes them to OtaImageReader.file
// (DclOtaUpdateService.ts:388-408, :573-574; OtaImageReader.ts:65-110);
// otherwise the payload size (OtaImageReader.ts:246-260) and, ahead of
// matter.js, which checks it only at extraction (:262-278), the header's
// payload digest, so a truncated or corrupted local image is refused before
// it is offered over BDX (W4-ctlint-03, BD-Matter-OtaImageDigestAtStore).
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

// recordMeta records the specification version of the stored image name;
// 0 removes the record. The record is optional, so a failure is logged and
// fails nothing. Mirrors matter.js #recordMetadata
// (DclOtaUpdateService.ts:468-491 at 862ad75f); an unset value is 0 here,
// the one invalid value a uint32 can carry (isSpecificationVersion, :67-69).
func (i *Images) recordMeta(ctx context.Context, name string, spec uint32) {
	var err error
	if spec == 0 {
		err = i.blobs.Delete(ctx, name+metaSuffix)
	} else {
		err = i.blobs.Put(ctx, name+metaSuffix, []byte(strconv.FormatUint(uint64(spec), 10)))
	}
	if err != nil {
		i.log.Warn("ota.images.meta_not_stored", slog.String("file", name), slog.Any("err", err))
	}
}

// readMeta is the recorded specification version of the stored image
// name, 0 when none is recorded. A record that cannot be read or holds no
// valid version is removed, so it is reported once. Mirrors matter.js
// #readMetadata (DclOtaUpdateService.ts:493-524 at 862ad75f).
func (i *Images) readMeta(ctx context.Context, name string) uint32 {
	m, err := i.blobs.Get(ctx, name+metaSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return 0
	}
	if err == nil {
		v, perr := strconv.ParseUint(string(m), 10, 32)
		if perr == nil && v > 0 {
			return uint32(v)
		}
		i.log.Warn("ota.images.meta_invalid_removed", slog.String("file", name))
	} else {
		i.log.Warn("ota.images.meta_unreadable_removed", slog.String("file", name), slog.Any("err", err))
	}
	if err := i.blobs.Delete(ctx, name+metaSuffix); err != nil {
		i.log.Warn("ota.images.meta_not_removed", slog.String("file", name), slog.Any("err", err))
	}
	return 0
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
	b, err := i.openBlob(ctx, name)
	if err != nil {
		return StoredImage{}, false
	}
	defer closeBlob(b)
	h, _, _, err := otaimage.ReadHeader(blobReader(b))
	if err != nil {
		i.log.Warn("ota.images.unreadable", slog.String("file", name), slog.Any("err", err))
		return StoredImage{}, false
	}
	st := StoredImage{
		Filename: name, VendorID: h.VendorID, ProductID: h.ProductID, SoftwareVersion: h.SoftwareVersion,
		SoftwareVersionString: h.SoftwareVersionString, MinApplicable: h.MinApplicableSoftwareVersion,
		// A host-chosen "FFF1.8000.PROD.3" is the prod image: the pattern
		// admits any case (:55), the Mode constants are lower case.
		MaxApplicable: h.MaxApplicableSoftwareVersion, Mode: Mode(strings.ToLower(parts[2])), Size: int(b.Size()), //nolint:gosec // a stored file's size

	}
	st.SpecificationVersion = i.readMeta(ctx, name)
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
		if strings.HasSuffix(name, metaSuffix) || !filenamePattern.MatchString(name) || i.isHidden(name) {
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

// DeleteOptions select the stored images [Images.Delete] removes —
// delete's options (DclOtaUpdateService.ts:1187-1194 at 862ad75f).
type DeleteOptions struct {
	// Filename names one stored image ("fff1.8000.prod.3"); the other
	// fields are then not read.
	Filename string
	// VendorID selects the vendor's images; required without Filename.
	VendorID *uint16
	// ProductID narrows them to one product.
	ProductID *uint16
	// Mode narrows them to one storage mode; empty is every mode.
	Mode Mode
	// IsProduction, without Mode, is ModeProd (true) or ModeTest (false),
	// matter.js's deprecated isProduction (:1203-1206).
	IsProduction *bool
}

// Delete removes the stored images opts selects, each with its recorded
// specification version, and returns how many it removed — matter.js
// DclOtaUpdateService.delete (:1187-1275 at 862ad75f). A filename that is
// not an OTA filename is refused; one that names no stored image removes
// nothing and is no error (:1208-1221). Without a filename the vendor is
// required (:1245-1247). A ProductID narrows the selection with or without
// a mode, where matter.js narrows by product only together with a mode
// (BD-Matter-OtaImages-DeleteProductWithoutMode).
func (i *Images) Delete(ctx context.Context, opts DeleteOptions) (int, error) {
	mode := opts.Mode
	if mode == "" && opts.IsProduction != nil {
		mode = ModeTest
		if *opts.IsProduction {
			mode = ModeProd
		}
	}
	if opts.Filename == "" && opts.VendorID == nil {
		return 0, fmt.Errorf("%w: either filename or vendor ID must be provided to delete files", ErrOTA)
	}
	if opts.Filename != "" && !filenamePattern.MatchString(opts.Filename) {
		return 0, fmt.Errorf("%w: invalid OTA filename format: %s", ErrOTA, opts.Filename)
	}
	names, err := i.blobs.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w: list stored images: %w", ErrOTA, err)
	}
	deleted := 0
	for _, name := range names {
		if strings.HasSuffix(name, metaSuffix) || strings.HasSuffix(name, originSuffix) || !filenamePattern.MatchString(name) {
			continue
		}
		if opts.Filename != "" {
			if name != opts.Filename {
				continue
			}
		} else if !deleteSelects(name, opts.VendorID, opts.ProductID, mode) {
			continue
		}
		_ = i.blobs.Delete(ctx, name+metaSuffix)
		_ = i.blobs.Delete(ctx, name+originSuffix)
		if err := i.blobs.Delete(ctx, name); err != nil {
			return deleted, fmt.Errorf("%w: delete %s: %w", ErrOTA, name, err)
		}
		deleted++
	}
	if deleted > 0 {
		i.log.Info("ota.images.deleted", slog.Int("count", deleted))
	}
	return deleted, nil
}

// deleteSelects reports whether the stored image name ("<vid>.<pid>.<mode>.
// <version>", hex ids in any case) is of vid and, when given, pid and mode.
func deleteSelects(name string, vid, pid *uint16, mode Mode) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return false
	}
	v, err := strconv.ParseUint(parts[0], 16, 16)
	if err != nil || v != uint64(*vid) {
		return false
	}
	if pid != nil {
		if p, err := strconv.ParseUint(parts[1], 16, 16); err != nil || p != uint64(*pid) {
			return false
		}
	}
	return mode == "" || Mode(strings.ToLower(parts[2])) == mode
}

// Open opens the stored image named filename for a BDX transfer
// (fileDesignatorForUpdate, :1156-1173). A name that is not an OTA
// filename is refused as matter.js refuses it and, as no stored image has
// it, wraps fs.ErrNotExist; a missing image wraps the Blobs error.
func (i *Images) Open(ctx context.Context, filename string) (bdx.Blob, error) {
	if !filenamePattern.MatchString(filename) {
		return nil, fmt.Errorf("%w: invalid OTA filename format: %s: %w", ErrOTA, filename, fs.ErrNotExist)
	}
	if i.isHidden(filename) {
		return nil, fmt.Errorf("%w: OTA file not found: %s: %w", ErrOTA, filename, fs.ErrNotExist)
	}
	if !i.servable(ctx, filename) {
		i.log.Warn("dcl.ota.no_checksum", slog.String("file", filename),
			slog.String("message", fmt.Sprintf("OTA image %s: the stored image is not bound to a ledger checksum; not served", filename)))
		return nil, fmt.Errorf("%w: stored OTA image %s is not bound to a ledger checksum; ImagesConfig.AllowUnverifiedLedgerImages admits it: %w",
			ErrOTA, filename, fs.ErrNotExist)
	}
	b, err := i.openBlob(ctx, filename)
	if err != nil {
		return nil, fmt.Errorf("%w: OTA file not found: %s: %w", ErrOTA, filename, err)
	}
	return b, nil
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
			if !i.servable(ctx, e.Filename) {
				i.log.Debug("ota.images.unverified_not_offered", slog.String("file", e.Filename))
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

// storedCheck is what the check of a stored image found —
// #checkStoredImage's verdict (DclOtaUpdateService.ts:429-451 at 862ad75f).
type storedCheck int

const (
	storedUnchecked storedCheck = iota
	storedValid
	storedInvalid
	// storedUnreadable is a stored image that cannot be read: a storage or
	// I/O error says nothing about the image content.
	storedUnreadable
)

// checkStored validates the stored image b (opened with getErr) against info
// as Store validates a new one — #checkStoredImage over #verifyUpdate
// (:388-451): the ledger's whole-file checksum and size when info carries
// them, the header's ids and version.
func (i *Images) checkStored(name string, b bdx.Blob, getErr error, info UpdateInfo) storedCheck {
	if getErr != nil {
		i.log.Info("ota.images.stored_unreadable", slog.String("file", name), slog.Any("err", getErr))
		return storedUnreadable
	}
	h, err := otaimage.ValidateFile(blobReader(b), storeValidation(&info))
	if err == nil {
		err = verifyHeader(h, info)
	}
	if err != nil {
		i.log.Info("ota.images.stored_invalid", slog.String("file", name), slog.Any("err", err))
		return storedInvalid
	}
	return storedValid
}

// DownloadUpdate makes info's image available in the store and returns
// its filename — downloadUpdate (DclOtaUpdateService.ts:615-720 at
// 862ad75f). A stored image that validates is used, unless force asks for
// a new download, and the specification version info carries is recorded
// for it; a stored image that fails validation is removed. Online a
// stored image that cannot be read, or one force replaces, is removed
// too and the ledger's image is downloaded, verified and stored.
//
// Offline (no ledger) nothing is downloaded and the call fails with
// [ErrOTA] unless a stored image validates: after removing one that failed
// validation, when none is stored, and — keeping the stored image, since
// nothing could replace it — when it cannot be read or force is set.
//
// A ledger update needs the ledger's whole-file checksum unless
// [ImagesConfig].AllowUnverifiedLedgerImages admits it without: an update
// whose record states none, and an update of a stored ledger image that
// carries no record of a checksum it was checked against, are refused
// with [ErrOTA] before anything else — nothing is downloaded, and the
// stored image is kept but not reused. An image the host supplied itself
// needs none.
//
// Calls for one file name, and [Images.Store] of it, run one at a time: a
// second call waits for the first and then finds the image the first
// stored (review II, R2-P6-conc-1901).
func (i *Images) DownloadUpdate(ctx context.Context, info UpdateInfo, force bool) (string, error) {
	name := fileName(info.VendorID, info.ProductID, modeOf(info.Source), info.SoftwareVersion)
	defer i.names.lock(name)()
	var unverified bool
	var why string
	switch {
	case hostSupplied(info):
	case info.ledger != nil:
		unverified, why = info.ledger.OTAChecksum == "", "the ledger record states no checksum (otaChecksum)"
	default:
		unverified, why = !i.vouched(ctx, name), "the stored image is not bound to a ledger checksum"
	}
	if unverified && !i.allowUnverified {
		i.log.Warn("dcl.ota.no_checksum", slog.String("file", name), slog.String("source", info.Source),
			slog.String("message", fmt.Sprintf("OTA image %s (%s): %s; not downloaded or served", name, info.SoftwareVersionString, why)))
		return "", fmt.Errorf("%w: OTA image %s (%s): %s, so the image is not downloaded or served; ImagesConfig.AllowUnverifiedLedgerImages admits it",
			ErrOTA, name, info.SoftwareVersionString, why)
	}
	got, err := i.downloadUpdate(ctx, info, force)
	if err == nil && unverified {
		i.log.Warn("dcl.ota.no_checksum_admitted", slog.String("file", name), slog.String("source", info.Source),
			slog.String("message", fmt.Sprintf("OTA image %s (%s): %s; admitted by AllowUnverifiedLedgerImages", got, info.SoftwareVersionString, why)))
	}
	return got, err
}

// downloadUpdate is DownloadUpdate past the checksum rule.
func (i *Images) downloadUpdate(ctx context.Context, info UpdateInfo, force bool) (string, error) {
	mode := modeOf(info.Source)
	name := fileName(info.VendorID, info.ProductID, mode, info.SoftwareVersion)
	b, getErr := i.openBlob(ctx, name)
	exists := !errors.Is(getErr, fs.ErrNotExist)
	check := storedUnchecked
	if exists && !force {
		check = i.checkStored(name, b, getErr, info)
	}
	if getErr == nil {
		closeBlob(b)
	}
	if check == storedValid {
		i.log.Info("ota.images.reused", slog.String("file", name))
		if info.SpecificationVersion != 0 {
			i.recordMeta(ctx, name, info.SpecificationVersion)
		}
		// Validated against info's ledger checksum, when it states one.
		if info.ledger != nil && !hostSupplied(info) && info.ledger.OTAChecksum != "" {
			i.recordOrigin(ctx, name, &info)
		}
		return name, nil
	}
	// Offline nothing can replace the image, so only one shown to be
	// invalid is removed (:646-651).
	keep := exists && i.offline && check != storedInvalid
	if exists && !keep {
		_ = i.blobs.Delete(ctx, name)
		_ = i.blobs.Delete(ctx, name+metaSuffix)
		_ = i.blobs.Delete(ctx, name+originSuffix)
	}
	if i.offline {
		switch {
		case check == storedUnreadable:
			return "", fmt.Errorf("%w: cannot read stored OTA image %s (%s) to validate it and the service is offline; the stored image is kept: %w",
				ErrOTA, name, info.SoftwareVersionString, getErr)
		case keep:
			return "", fmt.Errorf("%w: cannot force a download of OTA image %s (%s) while the service is offline; the stored image is kept",
				ErrOTA, name, info.SoftwareVersionString)
		}
		return "", fmt.Errorf("%w: no valid OTA image %s (%s) is stored and the service is offline, so none is downloaded",
			ErrOTA, name, info.SoftwareVersionString)
	}
	if info.ledger == nil {
		return "", fmt.Errorf("%w: no OTA URL provided in update info for %s (%s)", ErrOTA, name, info.SoftwareVersionString)
	}
	if s, ok := i.blobs.(BlobStreamer); ok {
		if err := i.downloadInto(ctx, s, name, info); err != nil {
			return "", err
		}
		return name, nil
	}
	var buf bytes.Buffer
	if err := i.fetch(ctx, info, &buf); err != nil {
		return "", fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if _, err := dcl.VerifyImage(bytes.NewReader(buf.Bytes()), *info.ledger, i.log); err != nil {
		return "", fmt.Errorf("%w: %w", ErrOTA, err)
	}
	// Store's checks, under the lock DownloadUpdate holds for name: the
	// header matches info, so Store would pick the same name.
	if _, err := checkImage(buf.Bytes(), &info); err != nil {
		return "", err
	}
	if _, err := i.store(ctx, name, buf.Bytes(), &info); err != nil {
		return "", err
	}
	return name, nil
}

// downloadInto is DownloadUpdate's download through a [BlobStreamer]
// (R2-P5-port-14): the ledger image is written into the file name as it
// arrives, committed, and read back for the checks the buffered path runs
// on its bytes before it stores them — the ledger's checksum and size
// (dcl.VerifyImage), then [Images.Store]'s file and header checks — with
// the same errors. A refused check removes the file and its record; until
// the checks pass Find and Open do not offer it. The specification
// version info carries is recorded as Store records it.
func (i *Images) downloadInto(ctx context.Context, s BlobStreamer, name string, info UpdateInfo) error {
	defer i.hide(name)()
	if err := i.forgetOrigin(ctx, name); err != nil {
		return err
	}
	w, err := s.CreateBlob(ctx, name)
	if err != nil {
		return fmt.Errorf("%w: store %s: %w", ErrOTA, name, err)
	}
	if err := i.fetch(ctx, info, w); err != nil {
		_ = w.Abort()
		return fmt.Errorf("%w: %w", ErrOTA, err)
	}
	if err := w.Commit(); err != nil {
		_ = w.Abort()
		return fmt.Errorf("%w: store %s: %w", ErrOTA, name, err)
	}
	if err := i.verifyStored(ctx, s, name, info); err != nil {
		_ = i.blobs.Delete(ctx, name)
		_ = i.blobs.Delete(ctx, name+metaSuffix)
		_ = i.blobs.Delete(ctx, name+originSuffix)
		return err
	}
	i.recordMeta(ctx, name, info.SpecificationVersion)
	i.recordOrigin(ctx, name, &info)
	i.log.Debug("ota.images.stored", slog.String("file", name))
	return nil
}

// verifyStored reads the committed file name back and checks it against
// info's ledger record and header as the buffered path checks its bytes.
func (i *Images) verifyStored(ctx context.Context, s BlobStreamer, name string, info UpdateInfo) error {
	b, err := s.OpenBlob(ctx, name)
	if err != nil {
		return fmt.Errorf("%w: store %s: %w", ErrOTA, name, err)
	}
	defer closeBlob(b)
	if _, err := dcl.VerifyImage(blobReader(b), *info.ledger, i.log); err != nil {
		return fmt.Errorf("%w: %w", ErrOTA, err)
	}
	h, err := otaimage.ValidateFile(blobReader(b), storeValidation(&info))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrOTA, err)
	}
	return verifyHeader(h, info)
}

// headerPrefix is the OTA file's fixed prefix: file identifier, total
// size, header size (otaimage.ReadHeader reads it first).
const headerPrefix = 4 + 8 + 4

// errHeaderRead ends a download once headerWriter holds the header.
var errHeaderRead = errors.New("softwareupdate: the OTA header is read")

// headerWriter keeps the first bytes of an OTA file up to the end of its
// header — the prefix and the header size the prefix states — and then
// refuses more with errHeaderRead, which ends the download. It keeps at
// most the prefix and otaimage.MaxHeaderSize bytes; a header size above
// that ends it after the prefix, which otaimage.ReadHeader refuses. A file
// shorter than its header is kept whole.
type headerWriter struct{ buf []byte }

// done reports whether buf holds the header the prefix states.
func (h *headerWriter) done() bool {
	if len(h.buf) < headerPrefix {
		return false
	}
	n := binary.LittleEndian.Uint32(h.buf[12:headerPrefix])
	return n > otaimage.MaxHeaderSize || len(h.buf) >= headerPrefix+int(n)
}

func (h *headerWriter) Write(p []byte) (int, error) {
	if h.done() {
		return 0, errHeaderRead
	}
	take := min(len(p), headerPrefix+otaimage.MaxHeaderSize-len(h.buf))
	h.buf = append(h.buf, p[:take]...)
	if h.done() {
		return take, errHeaderRead
	}
	return take, nil
}

// LocalImageOptions are the fields of a local image's [UpdateInfo] its
// header may not carry — updateInfoFromStream's options
// (DclOtaUpdateService.ts:841-863 at 862ad75f).
type LocalImageOptions struct {
	// MinApplicable is used when the header states none; nil is 0.
	MinApplicable *uint32
	// MaxApplicable is used when the header states none; nil is the
	// image's version minus one.
	MaxApplicable *uint32
	// SpecificationVersion is the Matter specification version the image
	// implements, recorded with it when it is stored; 0 is unknown.
	SpecificationVersion uint32
}

// UpdateInfoFromImage reads the header of the OTA image r and returns its
// update, of [SourceLocal], for [Images.Store] with [ModeLocal] — matter.js
// updateInfoFromStream (DclOtaUpdateService.ts:841-901 at 862ad75f): the
// header's ids, version and applicable range, opts where the header states
// none.
func UpdateInfoFromImage(r io.Reader, opts LocalImageOptions) (UpdateInfo, error) {
	h, _, _, err := otaimage.ReadHeader(r)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("%w: %w", ErrOTA, err)
	}
	u := UpdateInfo{
		VendorID: h.VendorID, ProductID: h.ProductID, SoftwareVersion: h.SoftwareVersion,
		SoftwareVersionString: h.SoftwareVersionString, MaxApplicable: h.SoftwareVersion - 1,
		SpecificationVersion: opts.SpecificationVersion, Source: SourceLocal,
	}
	switch {
	case h.MinApplicableSoftwareVersion != nil:
		u.MinApplicable = *h.MinApplicableSoftwareVersion
	case opts.MinApplicable != nil:
		u.MinApplicable = *opts.MinApplicable
	}
	switch {
	case h.MaxApplicableSoftwareVersion != nil:
		u.MaxApplicable = *h.MaxApplicableSoftwareVersion
	case opts.MaxApplicable != nil:
		u.MaxApplicable = *opts.MaxApplicable
	}
	if h.ReleaseNotesURL != nil {
		u.ReleaseNotesURL = *h.ReleaseNotesURL
	}
	return u, nil
}

// UpdateInfoFromURL reads the header of the OTA image at the HTTP(S) URL
// fileURL and returns its update, of [SourceLocal]; [Images.DownloadUpdate]
// then downloads it from that URL into [ModeLocal] — matter.js
// createUpdateInfoFromFile (DclOtaUpdateService.ts:924-981 at 862ad75f).
// The download ends once the header's bytes are in (at most
// [otaimage.MaxHeaderSize] and the 16-byte prefix); the rest of the image
// is not fetched.
// Another scheme is refused (a local file is [UpdateInfoFromImage]'s), and
// so is every URL offline, without a fetch. The host names the URL, so
// both downloads take [dcl.OTAVersions.DownloadHostURL]: http and a
// server on the local network are admitted, which a ledger record's URL
// is not.
func (i *Images) UpdateInfoFromURL(ctx context.Context, fileURL string, opts LocalImageOptions) (UpdateInfo, error) {
	scheme, _, _ := strings.Cut(fileURL, ":")
	if scheme = strings.ToLower(scheme); scheme != "https" && scheme != "http" {
		return UpdateInfo{}, fmt.Errorf("%w: only HTTP(S) URLs are supported; for a local file use UpdateInfoFromImage", ErrOTA)
	}
	if i.offline {
		return UpdateInfo{}, fmt.Errorf("%w: cannot read OTA image from %s: the service is offline, use UpdateInfoFromImage instead", ErrOTA, fileURL)
	}
	// Only the header is read: the download ends once its bytes are in
	// (review II, R2-P6-bound-1903).
	var head headerWriter
	if err := i.ledger.DownloadHostURL(ctx, dcl.SoftwareVersion{OTAURL: fileURL}, &head); err != nil && !errors.Is(err, errHeaderRead) {
		return UpdateInfo{}, fmt.Errorf("%w: failed to read OTA image from %s: %w", ErrOTA, fileURL, err)
	}
	u, err := UpdateInfoFromImage(bytes.NewReader(head.buf), opts)
	if err != nil {
		return UpdateInfo{}, fmt.Errorf("failed to read OTA image from %s: %w", fileURL, err)
	}
	u.ledger = &dcl.SoftwareVersion{
		VID: u.VendorID, PID: u.ProductID, SoftwareVersion: u.SoftwareVersion, SoftwareVersionString: u.SoftwareVersionString,
		SoftwareVersionValid: true, OTAURL: fileURL, MinApplicableSoftwareVersion: u.MinApplicable,
		MaxApplicableSoftwareVersion: u.MaxApplicable, ReleaseNotesURL: u.ReleaseNotesURL, SpecificationVersion: u.SpecificationVersion,
	}
	u.hostURL = true
	return u, nil
}
