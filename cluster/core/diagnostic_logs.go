// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	diaglogsdef "github.com/SukramJ/go-fabric/cluster/spec/diagnosticlogs"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// DiagnosticLogs implements the Matter DiagnosticLogs cluster (0x0032),
// mandatory on the root endpoint: RetrieveLogsRequest hands the logs of an
// intent over in the response or, for the BDX transfer protocol, over a
// BDX transfer this node initiates on the requesting session.
//
// matter.js answers nothing here — its DiagnosticLogsServer is an empty
// class — so chip is the behaviour reference, as CLAUDE.md provides for
// what matter.js does not model (ADR 0021): the server mirrors
// connectedhomeip src/app/clusters/diagnostic-logs-server/
// DiagnosticLogsCluster.cpp and BDXDiagnosticLogsProvider.cpp at the
// harness pin 6170af84 (DLC and BDLP below), its statuses and limits line
// by line. The log content comes from a [LogCollector] — chip's
// DiagnosticLogsProviderDelegate — or from a [LogProvider], which hands
// the whole log over as one byte slice.
type DiagnosticLogs struct {
	mu        sync.RWMutex
	collector LogCollector
	exchanges BDXExchangeOpener
	bootEpoch time.Time
	logger    *slog.Logger
	// busy is set while a BDX transfer runs: chip has one
	// BDXDiagnosticLogsProvider (DLC:62-64, :146-150).
	busy bool
}

// LogProvider supplies the log of an intent as one byte slice — the
// simplest host port. Return nil or empty for "no logs" (NoLogs); an error
// is answered Denied, as chip answers a delegate's failure (DLC:93).
//
// Implementations MUST be safe for concurrent calls. The returned slice
// may be retained by the server; do not mutate it after surrender.
type LogProvider interface {
	Logs(ctx context.Context, intent uint8) ([]byte, error)
}

// LogTimestamps are the timestamps of the oldest log entry a response
// carries (chip's outTimeStamp / outTimeSinceBoot, DiagnosticLogsProviderDelegate.h:
// 49-50, :117-118): UTC in Matter epoch-us and the time since boot in
// systime-us; nil leaves the field out.
type LogTimestamps struct {
	UTC       *uint64
	SinceBoot *uint64
}

// LogCollector is chip's DiagnosticLogsProviderDelegate
// (src/app/clusters/diagnostic-logs-server/DiagnosticLogsProviderDelegate.h:
// 37-123) in Go form: the size of an intent's logs, the newest of them
// for a response, and a collection session that streams them for BDX.
// Implementations MUST be safe for concurrent calls.
type LogCollector interface {
	// GetSizeForIntent returns the size of the intent's logs in bytes, 0
	// for none (:102-110). An error is answered Denied.
	GetSizeForIntent(ctx context.Context, intent uint8) (uint64, error)
	// GetLogForIntent returns the newest log entries of the intent, at
	// most max bytes, and the oldest returned entry's timestamps
	// (:112-122). [ErrNoLogs] is answered NoLogs, any other error Denied
	// (DLC:92-93).
	GetLogForIntent(ctx context.Context, intent uint8, maxBytes int) ([]byte, LogTimestamps, error)
	// StartLogCollection starts collecting the intent's logs for a BDX
	// transfer and returns the session and the oldest entry's timestamps
	// (:44-54). An error is answered Denied (DLC:152-157).
	StartLogCollection(ctx context.Context, intent uint8) (LogSession, LogTimestamps, error)
}

// LogSession is one log collection (chip's LogSessionHandle with
// CollectLog and EndLogCollection, DiagnosticLogsProviderDelegate.h:56-100).
type LogSession interface {
	// CollectLog fills buf with the next chunk and reports whether it is
	// the end of the log (:90-100).
	CollectLog(ctx context.Context, buf []byte) (n int, endOfLog bool, err error)
	// EndLogCollection ends the session; err is why — nil for a completed
	// transfer (:69-88).
	EndLogCollection(err error)
}

// BDXExchangeOpener opens a new exchange on the secure session a command
// arrived on, for a BDX transfer this node initiates — chip's
// exchangeMgr->NewContext(sessionHandle) (BDLP:67-70). *bridge.Bridge
// implements it.
type BDXExchangeOpener interface {
	OpenSessionExchange(ctx context.Context, sessionID uint16) (bdx.Exchange, error)
}

// ErrNoLogs is a [LogCollector]'s "no logs of this intent" (chip's
// CHIP_ERROR_NOT_FOUND, DLC:92).
var ErrNoLogs = errors.New("core: no logs")

// Cluster ID, revision and command ids: the generated definition's
// (cluster/spec/diagnosticlogs, ADR 0013, from matter.js
// diagnostic-logs.element.ts). The command lists, the statuses of a
// refused write, the request the bridge decodes and the response codec
// come from it as well.
const (
	diaglogsClusterID       = diaglogsdef.ClusterID
	diaglogsClusterRevision = diaglogsdef.Revision

	diaglogsCmdRetrieveLogsRequest  = diaglogsdef.CmdRetrieveLogsRequest
	diaglogsCmdRetrieveLogsResponse = diaglogsdef.CmdRetrieveLogsResponse

	// MatterDiagnosticLogsInlineCap is the largest LogContent a response
	// carries: chip's kMaxLogContentSize (src/protocols/bdx/
	// DiagnosticLogs.h:29-30, 1024), the field's `octstr<= 1024` (Matter
	// §11.11.7.2).
	MatterDiagnosticLogsInlineCap = 1024

	// MaxLogFileDesignatorLength is chip's kMaxFileDesignatorLen
	// (DiagnosticLogs.h:26-27, 32); the generated request decode answers
	// a longer TransferFileDesignator with ConstraintError, as DLC:125
	// does.
	MaxLogFileDesignatorLength = 32

	// logTransferBlockSize is CHIP_CONFIG_BDX_LOG_TRANSFER_MAX_BLOCK_SIZE
	// (src/lib/core/CHIPConfig.h:1970-1972, 1024), the block size the
	// SendInit offers (BDLP:39, :74).
	logTransferBlockSize = 1024
	// logTransferTimeout is kBdxTimeout (BDLP:36-37, five minutes).
	logTransferTimeout = 5 * time.Minute
)

// IntentEnum values (Matter §11.11.5.1).
const (
	IntentEndUserSupport = uint8(diaglogsdef.IntentEndUserSupport)
	IntentNetworkDiag    = uint8(diaglogsdef.IntentNetworkDiag)
	IntentCrashLogs      = uint8(diaglogsdef.IntentCrashLogs)
)

// StatusEnum values (Matter §11.11.5.2).
const (
	LogStatusSuccess   = uint8(diaglogsdef.StatusSuccess)
	LogStatusExhausted = uint8(diaglogsdef.StatusExhausted)
	LogStatusNoLogs    = uint8(diaglogsdef.StatusNoLogs)
	LogStatusBusy      = uint8(diaglogsdef.StatusBusy)
	LogStatusDenied    = uint8(diaglogsdef.StatusDenied)
)

// RetrieveLogsResponse mirrors the response shape (Matter §11.11.7.2).
type RetrieveLogsResponse struct {
	Status     uint8
	LogContent []byte
	// UTCTimeStamp is epoch-us (diagnostic-logs.element.ts:36):
	// microseconds since the Matter epoch 2000-01-01T00:00:00Z, the same
	// unit TimeSynchronization.UTCTime reports.
	UTCTimeStamp uint64
	// TimeSinceBoot is systime-us (diagnostic-logs.element.ts:37):
	// microseconds since the boot epoch.
	TimeSinceBoot uint64

	// omitUTC and omitBoot leave the timestamps out: a failure carries
	// none (DLC:44-49), a success those the collector has (BDLP:252-263).
	omitUTC, omitBoot bool
}

// HasTimestamps reports which of the two timestamps the response carries.
func (r RetrieveLogsResponse) HasTimestamps() (utc, sinceBoot bool) { return !r.omitUTC, !r.omitBoot }

// EncodeTLV implements spec.Encodable with the generated response codec
// (diagnostic-logs.element.ts RetrieveLogsResponse): Status, LogContent,
// and the two optional timestamps when the response carries them.
func (r RetrieveLogsResponse) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	out := diaglogsdef.RetrieveLogsResponse{Status: diaglogsdef.StatusEnum(r.Status), LogContent: r.LogContent}
	if !r.omitUTC {
		utc := r.UTCTimeStamp
		out.UtcTimeStamp = &utc
	}
	if !r.omitBoot {
		boot := r.TimeSinceBoot
		out.TimeSinceBoot = &boot
	}
	if out.LogContent == nil {
		out.LogContent = []byte{}
	}
	out.EncodeTLV(enc, tag)
}

// ResponseCommand implements spec.ResponsePayload: RetrieveLogsResponse
// (0x01).
func (RetrieveLogsResponse) ResponseCommand() (clusterID, commandID uint32) {
	return diaglogsClusterID, diaglogsCmdRetrieveLogsResponse
}

// matterEpochMicros converts a wall-clock instant to Matter epoch-us
// (§A.2: epoch 2000-01-01 00:00:00 UTC, 946684800 s after the Unix
// epoch). A pre-epoch host clock yields 0 rather than wrapping.
func matterEpochMicros(t time.Time) uint64 {
	const matterEpochOffsetSec int64 = 946684800
	micros := t.UnixMicro() - matterEpochOffsetSec*1_000_000
	if micros < 0 {
		return 0
	}
	return uint64(micros)
}

// NewDiagnosticLogs returns a cluster server with the boot epoch
// stamped to "now" — the TimeSinceBoot a [LogProvider]'s response carries
// counts from this point. Operators that need a precise boot epoch can
// override it via [DiagnosticLogs.SetBootEpoch].
func NewDiagnosticLogs() *DiagnosticLogs {
	return &DiagnosticLogs{bootEpoch: time.Now(), logger: slog.Default()}
}

// AttachProvider wires a byte-slice log source; a provider that is also a
// [LogCollector] is used as one. Pass nil to revert to the NoLogs default
// (chip without a delegate, DLC:86, :127-131).
func (d *DiagnosticLogs) AttachProvider(p LogProvider) {
	var c LogCollector
	switch v := p.(type) {
	case nil:
	case LogCollector:
		c = v
	default:
		c = &sliceCollector{p: p, d: d}
	}
	d.AttachCollector(c)
}

// AttachCollector wires chip's delegate form of the log source; nil
// reverts to NoLogs.
func (d *DiagnosticLogs) AttachCollector(c LogCollector) {
	d.mu.Lock()
	d.collector = c
	d.mu.Unlock()
}

// AttachExchangeOpener wires the exchanges a BDX transfer runs on. Without
// one the node does not support BDX and answers a BDX request in the
// response, Exhausted, as chip does without BDX log transfer (DLC:141-144,
// :158-160).
func (d *DiagnosticLogs) AttachExchangeOpener(o BDXExchangeOpener) {
	d.mu.Lock()
	d.exchanges = o
	d.mu.Unlock()
}

// SetLogger sets the server's logger; nil is slog.Default().
func (d *DiagnosticLogs) SetLogger(l *slog.Logger) {
	if l == nil {
		l = slog.Default()
	}
	d.mu.Lock()
	d.logger = l
	d.mu.Unlock()
}

// SetBootEpoch overrides the timestamp a [LogProvider]'s TimeSinceBoot is
// computed against. Most operators won't need this; default is the
// cluster construction time.
func (d *DiagnosticLogs) SetBootEpoch(t time.Time) {
	d.mu.Lock()
	d.bootEpoch = t
	d.mu.Unlock()
}

// sliceCollector serves a [LogProvider] as a [LogCollector]: the slice is
// the whole log, its newest bytes the response's, and its timestamps the
// request's moment (the server's behaviour before the collector form).
type sliceCollector struct {
	p LogProvider
	d *DiagnosticLogs
}

func (s *sliceCollector) stamps() LogTimestamps {
	s.d.mu.RLock()
	boot := s.d.bootEpoch
	s.d.mu.RUnlock()
	utc := matterEpochMicros(time.Now())
	since := uint64(max(time.Since(boot).Microseconds(), 0)) //nolint:gosec // non-negative, systime-us
	return LogTimestamps{UTC: &utc, SinceBoot: &since}
}

func (s *sliceCollector) GetSizeForIntent(ctx context.Context, intent uint8) (uint64, error) {
	b, err := s.p.Logs(ctx, intent)
	return uint64(len(b)), err
}

func (s *sliceCollector) GetLogForIntent(ctx context.Context, intent uint8, maxBytes int) ([]byte, LogTimestamps, error) {
	b, err := s.p.Logs(ctx, intent)
	if err != nil {
		return nil, LogTimestamps{}, err
	}
	if len(b) == 0 {
		return nil, LogTimestamps{}, ErrNoLogs
	}
	if len(b) > maxBytes {
		// "the newest diagnostic log entries" (DiagnosticLogsProviderDelegate.h:112-113).
		b = b[len(b)-maxBytes:]
	}
	return append([]byte(nil), b...), s.stamps(), nil
}

func (s *sliceCollector) StartLogCollection(ctx context.Context, intent uint8) (LogSession, LogTimestamps, error) {
	b, err := s.p.Logs(ctx, intent)
	if err != nil {
		return nil, LogTimestamps{}, err
	}
	return &sliceSession{rest: append([]byte(nil), b...)}, s.stamps(), nil
}

type sliceSession struct{ rest []byte }

func (s *sliceSession) CollectLog(_ context.Context, buf []byte) (n int, endOfLog bool, err error) {
	n = copy(buf, s.rest)
	s.rest = s.rest[n:]
	return n, len(s.rest) == 0, nil
}

func (s *sliceSession) EndLogCollection(error) {}

// Compile-time assertions: DiagnosticLogs satisfies MatterClusterServer,
// the attribute-lister capability, and the command-lister capability.
var (
	_ contract.ClusterServer          = (*DiagnosticLogs)(nil)
	_ contract.ClusterAttributeLister = (*DiagnosticLogs)(nil)
	_ contract.ClusterCommandLister   = (*DiagnosticLogs)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (d *DiagnosticLogs) MatterClusterID() uint32 { return diaglogsClusterID }

// MatterRead implements [contract.ClusterServer]. The cluster
// has no readable attributes other than the global ones (DLC:166-179).
func (d *DiagnosticLogs) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return diaglogsInst.ReadGlobal(attrID)
	}
	return nil, false
}

// MatterWrite always rejects — DiagnosticLogs has no writable attributes.
func (d *DiagnosticLogs) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := diaglogsInst.ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("matter: DiagnosticLogs is read-only (got attr 0x%04X)", attrID), status: err}
}

// diaglogsInst is the definition bound to what the server serves: no
// attribute but the globals, RetrieveLogsRequest and its response.
var diaglogsInst = mustInstance(diaglogsdef.Definition, spec.Options{})

// diagLogsError is an IM status answer to the command.
type diagLogsError struct {
	code im.StatusCode
	msg  string
}

func (e diagLogsError) Error() string                   { return "matter: DiagnosticLogs: " + e.msg }
func (e diagLogsError) MatterStatusCode() im.StatusCode { return e.code }

// MatterInvoke implements RetrieveLogsRequest (DLC:181-210): an intent or
// protocol outside its enum is InvalidCommand (:194-197), the response
// payload protocol answers in the response (:199-203), BDX starts a
// transfer (:205).
func (d *DiagnosticLogs) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID != diaglogsCmdRetrieveLogsRequest {
		return nil, im.UnsupportedCommandf("matter: DiagnosticLogs command 0x%02X not supported", cmdID)
	}
	req := decodeRetrieveLogsRequest(fields)
	if req.Intent > diaglogsdef.IntentCrashLogs || req.RequestedProtocol > diaglogsdef.TransferProtocolBdx {
		return nil, diagLogsError{im.StatusInvalidCommand, "intent or protocol outside its enum"}
	}
	intent := uint8(req.Intent)
	if req.RequestedProtocol == diaglogsdef.TransferProtocolResponsePayload {
		return d.responsePayload(ctx, intent, LogStatusSuccess), nil
	}
	return d.logRequestForBdx(ctx, intent, req.TransferFileDesignator)
}

// responsePayload is HandleLogRequestForResponsePayload with
// LogRequestHandler::Process (DLC:84-115): NoLogs without a collector or
// logs, Denied when the collector fails, else the newest logs with the
// status of success and the collector's timestamps.
func (d *DiagnosticLogs) responsePayload(ctx context.Context, intent, statusOnSuccess uint8) RetrieveLogsResponse {
	d.mu.RLock()
	c := d.collector
	d.mu.RUnlock()
	if c == nil {
		return failure(LogStatusNoLogs) // :86
	}
	size, err := c.GetSizeForIntent(ctx, intent)
	switch {
	case err != nil:
		return failure(LogStatusDenied)
	case size == 0:
		return failure(LogStatusNoLogs) // :88
	}
	content, ts, err := c.GetLogForIntent(ctx, intent, MatterDiagnosticLogsInlineCap)
	switch {
	case errors.Is(err, ErrNoLogs):
		return failure(LogStatusNoLogs) // :92
	case err != nil:
		return failure(LogStatusDenied) // :93
	}
	if len(content) > MatterDiagnosticLogsInlineCap {
		content = content[len(content)-MatterDiagnosticLogsInlineCap:]
	}
	return success(statusOnSuccess, content, ts) // :113
}

// failure is DLC:44-49: the status alone.
func failure(status uint8) RetrieveLogsResponse {
	return RetrieveLogsResponse{Status: status, LogContent: []byte{}, omitUTC: true, omitBoot: true}
}

// success is DLC:51-60 and BDLP:250-263: the content and the timestamps
// the collector reported.
func success(status uint8, content []byte, ts LogTimestamps) RetrieveLogsResponse {
	r := RetrieveLogsResponse{Status: status, LogContent: content, omitUTC: ts.UTC == nil, omitBoot: ts.SinceBoot == nil}
	if r.LogContent == nil {
		r.LogContent = []byte{}
	}
	if ts.UTC != nil {
		r.UTCTimeStamp = *ts.UTC
	}
	if ts.SinceBoot != nil {
		r.TimeSinceBoot = *ts.SinceBoot
	}
	return r
}

// logRequestForBdx is HandleLogRequestForBdx (DLC:117-163).
func (d *DiagnosticLogs) logRequestForBdx(ctx context.Context, intent uint8, designator *string) (any, error) {
	// :121-125: no designator is InvalidCommand; one longer than 32 bytes
	// ConstraintError (the generated decode already refuses it).
	if designator == nil {
		return nil, diagLogsError{im.StatusInvalidCommand, "BDX requested without a TransferFileDesignator"}
	}
	if len(*designator) > MaxLogFileDesignatorLength {
		return nil, diagLogsError{im.StatusConstraintError, "TransferFileDesignator longer than 32"}
	}
	d.mu.RLock()
	c, opener := d.collector, d.exchanges
	d.mu.RUnlock()
	if c == nil {
		return failure(LogStatusNoLogs), nil // :126-131
	}
	size, err := c.GetSizeForIntent(ctx, intent)
	switch {
	case err != nil:
		return failure(LogStatusDenied), nil //nolint:nilerr // a collector failure is the Denied status, not an IM error
	case size == 0:
		// :133-135: NoLogs through the response path, no BDX session.
		return d.responsePayload(ctx, intent, LogStatusNoLogs), nil
	case size <= MatterDiagnosticLogsInlineCap:
		// :137-140: the whole log fits the response — Exhausted, no BDX.
		return d.responsePayload(ctx, intent, LogStatusExhausted), nil
	case opener == nil:
		// :158-160: without BDX the response carries what fits, Exhausted.
		return d.responsePayload(ctx, intent, LogStatusExhausted), nil
	}
	d.mu.Lock()
	if d.busy {
		d.mu.Unlock()
		return failure(LogStatusBusy), nil // :146-150
	}
	d.busy = true
	logger := d.logger
	d.mu.Unlock()
	resp, ok := d.initializeTransfer(ctx, c, opener, intent, *designator, logger)
	if !ok {
		d.done()
		return failure(LogStatusDenied), nil // :152-157
	}
	return resp, nil
}

func (d *DiagnosticLogs) done() {
	d.mu.Lock()
	d.busy = false
	d.mu.Unlock()
}

// initializeTransfer is BDXDiagnosticLogsProvider::InitializeTransfer
// (BDLP:41-100) through the SendAccept (:167-174): the log collection
// starts, a new exchange on the command's session carries a SendInit —
// sender drive, 1024-byte blocks, the request's designator — and the
// command is answered Success with the collection's timestamps once the
// receiver accepted; the blocks follow once that answer is out. Any
// failure before the SendAccept — the receiver's StatusReport, a timeout —
// is reported false, answered Denied (:219-241).
func (d *DiagnosticLogs) initializeTransfer(ctx context.Context, c LogCollector, opener BDXExchangeOpener, intent uint8, designator string, logger *slog.Logger) (RetrieveLogsResponse, bool) {
	sessionID := InvokeSessionIDFromContext(ctx)
	if sessionID == 0 {
		return RetrieveLogsResponse{}, false // :51-59: a secure session with a peer
	}
	// The transfer outlives the command; it is bounded by its own idle
	// timeout (kBdxTimeout, :36-37).
	tctx := context.WithoutCancel(ctx)
	session, ts, err := c.StartLogCollection(ctx, intent) // :65
	if err != nil {
		logger.Info("diaglogs.bdx.start_failed", slog.String("err", err.Error()))
		return RetrieveLogsResponse{}, false
	}
	ex, err := opener.OpenSessionExchange(ctx, sessionID) // :67-70
	if err != nil {
		session.EndLogCollection(err)
		logger.Info("diaglogs.bdx.exchange_failed", slog.String("err", err.Error()))
		return RetrieveLogsResponse{}, false
	}
	m := bdx.NewMessenger(ex, logTransferTimeout)
	transfer, err := bdx.StartSend(tctx, m, bdx.SendOptions{ // :72-79
		TransferFileDesignator: bdx.FileDesignator(designator),
		Data:                   &logReader{ctx: tctx, session: session},
		PreferredDriverModes:   []bdx.DriverMode{bdx.SenderDrive},
		MaxBlockSize:           logTransferBlockSize,
	})
	if err != nil {
		_ = ex.Close()
		session.EndLogCollection(err)
		logger.Info("diaglogs.bdx.not_accepted", slog.String("err", err.Error()))
		return RetrieveLogsResponse{}, false
	}
	run := func() {
		go func() {
			_, err := transfer.Run(tctx)
			_ = ex.Close() // :271-278, Reset
			session.EndLogCollection(err)
			d.done()
			if err != nil {
				logger.Info("diaglogs.bdx.transfer_failed", slog.String("err", err.Error()))
			}
		}()
	}
	// The response goes out first, the blocks after it (:171-173, the
	// response before OnAckReceived's first block).
	if !im.DeferAfterResponse(ctx, run) {
		run()
	}
	return success(LogStatusSuccess, nil, ts), true
}

// logReader reads a log collection as a stream: each Read is one
// CollectLog (BDLP:176-210), the end of the log is io.EOF after its last
// chunk.
type logReader struct {
	ctx     context.Context //nolint:containedctx // the transfer's context, which the reader's caller does not carry
	session LogSession
	ended   bool
}

func (r *logReader) Read(p []byte) (int, error) {
	if r.ended {
		return 0, io.EOF
	}
	n, end, err := r.session.CollectLog(r.ctx, p)
	if err != nil {
		return n, err
	}
	if end {
		r.ended = true
		return n, io.EOF
	}
	return n, nil
}

// decodeRetrieveLogsRequest takes the command fields. The bridge decodes
// the request into the generated RetrieveLogsRequestRequest (a missing
// RequestedProtocol or an over-long TransferFileDesignator are answered
// there, as matter.js's request schema answers them); nil and a
// `map[uint8]any`-shaped struct are tolerated so test fixtures can drive
// the cluster without the full TLV decoder: they carry the intent alone
// and ask for the response payload. Unknown shapes default to
// EndUserSupport.
func decodeRetrieveLogsRequest(fields any) diaglogsdef.RetrieveLogsRequestRequest {
	switch f := fields.(type) {
	case diaglogsdef.RetrieveLogsRequestRequest:
		return f
	case *diaglogsdef.RetrieveLogsRequestRequest:
		if f != nil {
			return *f
		}
	case map[uint8]any:
		if raw, ok := f[0]; ok {
			if v, ok := raw.(uint64); ok {
				return diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEnum(v & 0xFF)} // intent is a 1-byte enum
			}
			if v, ok := raw.(uint8); ok {
				return diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEnum(v)}
			}
		}
	}
	return diaglogsdef.RetrieveLogsRequestRequest{Intent: diaglogsdef.IntentEndUserSupport}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
// Lists the command IDs the server handles via MatterInvoke.
// Mirrors matter.js packages/model/src/standard/elements/
// diagnostic-logs.element.ts accepted commands.
func (d *DiagnosticLogs) MatterAcceptedCommands() []uint32 {
	return diaglogsInst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// RetrieveLogsResponse.
func (d *DiagnosticLogs) MatterGeneratedCommands() []uint32 {
	return diaglogsInst.MatterGeneratedCommands()
}

// MatterReportable returns no attributes — nothing on this cluster
// changes outside command flow.
func (d *DiagnosticLogs) MatterReportable() []uint32 { return nil }

// MatterAttributes lists every DiagnosticLogs (0x0032) attribute the
// server implements via MatterRead. The cluster exposes no attributes
// beyond the globals (FeatureMap + ClusterRevision); Apple Home's HAP
// service rebuild is satisfied by an empty list here since globals are
// always injected by the dispatcher.
func (d *DiagnosticLogs) MatterAttributes() []uint32 { return nil }
