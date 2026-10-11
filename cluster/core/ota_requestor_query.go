// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/cluster/spec"
	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
	otadef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdaterequestor"
)

// This file is the requestor's query, download and apply flow:
// OtaSoftwareUpdateRequestorServer.ts:485-1285.

// queryTimer is the update query timer (internal.updateQueryTimer, :1399).
// A timer that fired stays set, no longer running, as matter.js's Timer
// does: #handleUpdatedDefaultOtaProviders tests for its absence (:365),
// #scheduleUpdateQuery for running (:496).
type queryTimer struct {
	stop    func() bool
	running bool
}

func (r *OTASoftwareUpdateRequestor) stopTimerLocked() {
	if r.timer != nil && r.timer.running {
		r.timer.stop()
		r.timer.running = false
	}
}

// scheduleUpdateQuery mirrors #scheduleUpdateQuery (:485-522): a query
// after delay (nil: keep a running timer, else the regular interval) and,
// with provider, against that provider once. It reports whether a query is
// scheduled when it returns.
func (r *OTASoftwareUpdateRequestor) scheduleUpdateQuery(delay *time.Duration, reason scheduleReason, provider *otadef.ProviderLocation) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if provider != nil {
		p := *provider
		r.selected = &p
		if reason == reasonBusy {
			r.providerRetries++
		}
	} else {
		r.providerRetries = 0
	}
	if r.timer != nil {
		if delay == nil && r.timer.running {
			return true // :496-499, already scheduled
		}
		r.stopTimerLocked()
		r.timer = nil
	}
	if !r.updatePossible || len(r.active) == 0 || r.stopped {
		return false
	}
	if delay == nil && r.state != otadef.UpdateStateIdle {
		r.logger.Info("matter.ota.schedule_refused", slog.Int("state", int(r.state)))
		return false
	}
	d := r.cfg.UpdateQueryInterval
	if delay != nil {
		d = *delay
	}
	r.logger.Info("matter.ota.query_scheduled", slog.Duration("in", d), slog.String("reason", reason.String()))
	t := &queryTimer{running: true}
	t.stop = r.clock.AfterFunc(d, func() {
		r.mu.Lock()
		current := r.timer == t && t.running
		if current {
			t.running = false
		}
		r.mu.Unlock()
		if current {
			r.goRun(r.performUpdateQuery)
		}
	})
	r.timer = t
	return true
}

// chooseUpdateProvider mirrors #chooseUpdateProvider (:525-550): a selected
// provider once, else the active provider queried longest ago — the list
// itself sorted, stable, by that timestamp, as matter.js sorts the state
// array in place.
func (r *OTASoftwareUpdateRequestor) chooseUpdateProvider() *otadef.ProviderLocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.selected != nil {
		p := *r.selected
		r.selected = nil
		return &p
	}
	if len(r.active) == 0 {
		return nil
	}
	ts := func(a activeProvider) int64 {
		if a.PreviousQueryTimestamp == nil {
			return 0
		}
		return *a.PreviousQueryTimestamp
	}
	sort.SliceStable(r.active, func(i, j int) bool { return ts(r.active[i]) < ts(r.active[j]) })
	p := r.active[0].Location
	return &p
}

// performUpdateQuery mirrors #performUpdateQuery (:568-582), then applies
// what the query prepared once it ended (#prepareApplyUpdate's onFinalize,
// :1259).
func (r *OTASoftwareUpdateRequestor) performUpdateQuery(ctx context.Context) {
	r.mu.Lock()
	if r.queryRunning {
		r.mu.Unlock()
		r.logger.Info("matter.ota.query_still_running")
		return
	}
	r.queryRunning = true
	r.mu.Unlock()
	r.queryForUpdate(ctx)
	r.mu.Lock()
	r.queryRunning = false
	pending := r.pendingApply
	r.pendingApply = nil
	r.mu.Unlock()
	if pending != nil {
		r.applyPreparedUpdate(ctx, pending.version, pending.file)
	}
}

// queryOutcome is QueryOutcome (:78-87).
type queryOutcome uint8

const (
	outcomeFinished queryOutcome = iota
	outcomeDelayed
	outcomeApplyPrepared
)

// queryForUpdate mirrors #queryForUpdate (:584-642).
func (r *OTASoftwareUpdateRequestor) queryForUpdate(ctx context.Context) {
	r.mu.Lock()
	applying := r.state == otadef.UpdateStateApplying
	r.mu.Unlock()
	if applying {
		r.logger.Info("matter.ota.query_skipped_applying")
		return
	}

	// :591-610: a valid file already downloaded is applied without asking.
	path := r.downloadPath()
	if _, err := os.Stat(path); err == nil {
		header, verr := r.validateUpdateFile(nil)
		if verr != nil {
			r.logger.Info("matter.ota.file_invalid", slog.String("err", verr.Error()))
			_ = os.Remove(path)
		} else {
			r.logger.Info("matter.ota.file_ready", slog.Uint64("version", uint64(header.SoftwareVersion)))
			r.prepareApplyUpdate(ctx, header.SoftwareVersion, path, nil, nil)
			return
		}
	}

	r.mu.Lock()
	r.progress = nil
	r.mu.Unlock()
	r.changed(otadef.AttrUpdateStateProgress)

	provider := r.chooseUpdateProvider()
	if provider == nil {
		r.logger.Info("matter.ota.no_provider")
		r.mu.Lock()
		r.stopTimerLocked()
		r.timer = nil
		r.mu.Unlock()
		return
	}

	r.updateState(otadef.UpdateStateQuerying, otadef.ChangeReasonSuccess, nil)

	outcome := outcomeFinished
	sess, err := r.connect(ctx, *provider)
	if err == nil {
		outcome, err = r.queryOtaProvider(ctx, sess, *provider)
	}
	if err != nil {
		// :632-637: handled as no update available.
		r.logger.Warn("matter.ota.provider_failed", slog.Uint64("provider", provider.ProviderNodeId), slog.String("err", err.Error()))
		r.markActiveOtaProviderNoUpdate(ctx, *provider)
	}
	if outcome == outcomeFinished {
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
	}
}

// connect mirrors #connectOtaProviderFor (:553-565).
func (r *OTASoftwareUpdateRequestor) connect(ctx context.Context, p otadef.ProviderLocation) (OtaProviderSession, error) {
	if r.cfg.Connector == nil {
		return nil, errors.New("core: OTA requestor has no provider connector")
	}
	return r.cfg.Connector.ConnectOtaProvider(ctx, p.FabricIndex, p.ProviderNodeId)
}

// updateDetails is what a provider offers (:754-761).
type updateDetails struct {
	imageURI              string
	softwareVersion       uint32
	softwareVersionString string
	updateToken           []byte
	userConsentNeeded     bool
	delayedActionTime     uint32
}

// updateAvailableFromProvider mirrors #updateAvailableFromProvider
// (:650-762): QueryImage, and every answer but UpdateAvailable handled
// here. It returns the offered update, or nil with the outcome.
func (r *OTASoftwareUpdateRequestor) updateAvailableFromProvider(ctx context.Context, query provdef.QueryImageRequest, sess OtaProviderSession, loc otadef.ProviderLocation) (*updateDetails, queryOutcome, error) {
	node, err := sess.Invoke(ctx, loc.Endpoint, provdef.ClusterID, provdef.CmdQueryImage, query)
	if err != nil {
		return nil, outcomeFinished, err
	}
	if node == nil {
		return nil, outcomeFinished, errors.New("QueryImage answered without a QueryImageResponse")
	}
	resp, err := spec.DecodeStruct[provdef.QueryImageResponse](*node)
	if err != nil {
		return nil, outcomeFinished, err
	}
	var delayed uint32
	if resp.DelayedActionTime != nil {
		delayed = *resp.DelayedActionTime
	}

	switch resp.Status {
	case provdef.StatusBusy, provdef.StatusNotAvailable:
		// :669-690: a Busy provider is asked again, up to three times,
		// after its delay floored to the minimum query interval.
		if resp.Status == provdef.StatusBusy {
			r.mu.Lock()
			retries := r.providerRetries
			r.mu.Unlock()
			d := max(time.Duration(delayed)*time.Second, r.cfg.MinimumQueryInterval)
			if retries < otaMaxBusyRetries && r.scheduleUpdateQuery(&d, reasonBusy, &loc) {
				r.updateState(otadef.UpdateStateDelayedOnQuery, otadef.ChangeReasonDelayByProvider, nil)
				return nil, outcomeDelayed, nil
			}
		}
		r.markActiveOtaProviderNoUpdate(ctx, loc)
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
		return nil, outcomeFinished, nil
	case provdef.StatusUpdateAvailable:
	case provdef.StatusDownloadProtocolNotSupported:
		// :691-698: no need to query this one again.
		r.logger.Info("matter.ota.provider_removed_no_protocol", slog.Uint64("provider", loc.ProviderNodeId))
		r.removeOtaProvider(ctx, loc)
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
		return nil, outcomeFinished, nil
	}

	return r.offeredUpdate(ctx, resp, delayed, query, loc)
}

// offeredUpdate holds an UpdateAvailable answer to the checks of
// #updateAvailableFromProvider (:701-761).
func (r *OTASoftwareUpdateRequestor) offeredUpdate(ctx context.Context, resp provdef.QueryImageResponse, delayed uint32, query provdef.QueryImageRequest, loc otadef.ProviderLocation) (*updateDetails, queryOutcome, error) {
	// :701-711: the mandatory fields of an UpdateAvailable answer.
	if resp.ImageUri == nil || resp.SoftwareVersion == nil || resp.SoftwareVersionString == nil || resp.UpdateToken == nil {
		r.logger.Info("matter.ota.response_incomplete")
		r.markActiveOtaProviderNoUpdate(ctx, loc)
		r.resetStateToIdle(otadef.ChangeReasonFailure)
		return nil, outcomeFinished, nil
	}
	uri := *resp.ImageUri
	// :713-726: a BDX URI names the provider itself.
	if strings.HasPrefix(uri, "bdx://") {
		_, source, err := bdx.ParseBdxURI(uri)
		if err != nil {
			return nil, outcomeFinished, err
		}
		if source != loc.ProviderNodeId {
			r.logger.Info("matter.ota.uri_node_mismatch", slog.Uint64("uri_node", source), slog.Uint64("provider", loc.ProviderNodeId))
			r.markActiveOtaProviderNoUpdate(ctx, loc)
			r.resetStateToIdle(otadef.ChangeReasonFailure)
			return nil, outcomeFinished, nil
		}
	}
	// :728-736: only a newer version.
	if *resp.SoftwareVersion <= query.SoftwareVersion {
		r.logger.Info("matter.ota.version_not_newer", slog.Uint64("offered", uint64(*resp.SoftwareVersion)), slog.Uint64("current", uint64(query.SoftwareVersion)))
		r.markActiveOtaProviderNoUpdate(ctx, loc)
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
		return nil, outcomeFinished, nil
	}
	consent := resp.UserConsentNeeded != nil && *resp.UserConsentNeeded
	if consent {
		// :738-752.
		if !r.cfg.CanConsent {
			r.logger.Info("matter.ota.consent_impossible")
			r.markActiveOtaProviderNoUpdate(ctx, loc)
			r.resetStateToIdle(otadef.ChangeReasonSuccess)
			return nil, outcomeFinished, nil
		}
		if r.cfg.NodeInformation().LocalConfigDisabled {
			r.logger.Info("matter.ota.consent_local_config_disabled")
			r.resetStateToIdle(otadef.ChangeReasonFailure)
			return nil, outcomeFinished, nil
		}
	}
	return &updateDetails{
		imageURI: uri, softwareVersion: *resp.SoftwareVersion, softwareVersionString: *resp.SoftwareVersionString,
		updateToken: *resp.UpdateToken, userConsentNeeded: consent, delayedActionTime: delayed,
	}, outcomeFinished, nil
}

// sleep waits d unless ctx ends first (matter.js Time.sleep with the
// cancellable internal.updateDelayPromise, :807-812, :1307).
func (r *OTASoftwareUpdateRequestor) sleep(ctx context.Context, d time.Duration) error {
	done := make(chan struct{})
	stop := r.clock.AfterFunc(d, func() { close(done) })
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		stop()
		return ctx.Err()
	}
}

// validateApplyUpdate mirrors #validateApplyUpdate (:767-837):
// ApplyUpdateRequest, then Proceed (after the delay the provider names,
// capped at 24 h), AwaitNextAction (ask again after the delay floored to
// the minimum apply delay, capped at 24 h) or Discontinue.
func (r *OTASoftwareUpdateRequestor) validateApplyUpdate(ctx context.Context, req provdef.ApplyUpdateRequestRequest, sess OtaProviderSession, loc otadef.ProviderLocation) bool {
	r.setProgress(nil)
	v := req.NewVersion
	r.updateState(otadef.UpdateStateApplying, otadef.ChangeReasonSuccess, &v)

	node, err := sess.Invoke(ctx, loc.Endpoint, provdef.ClusterID, provdef.CmdApplyUpdateRequest, req)
	var resp provdef.ApplyUpdateResponse
	if err == nil {
		if node == nil {
			err = errors.New("ApplyUpdateRequest answered without an ApplyUpdateResponse")
		} else {
			resp, err = spec.DecodeStruct[provdef.ApplyUpdateResponse](*node)
		}
	}
	if err != nil {
		r.logger.Info("matter.ota.apply_request_failed", slog.String("err", err.Error()))
		r.resetStateToIdle(otadef.ChangeReasonFailure)
		return false
	}

	switch resp.Action {
	case provdef.ApplyUpdateActionDiscontinue:
		if err := os.Remove(r.downloadPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.logger.Warn("matter.ota.file_delete_failed", slog.String("err", err.Error()))
		}
		r.resetStateToIdle(otadef.ChangeReasonSuccess)
		return false
	case provdef.ApplyUpdateActionAwaitNextAction:
		r.updateState(otadef.UpdateStateDelayedOnApply, otadef.ChangeReasonDelayByProvider, nil)
		d := min(max(time.Duration(resp.DelayedActionTime)*time.Second, r.cfg.MinimumApplyDelay), otaMaxDelay)
		if r.sleep(ctx, d) != nil {
			return false
		}
		return r.validateApplyUpdate(ctx, req, sess, loc)
	case provdef.ApplyUpdateActionProceed:
	default:
		r.logger.Info("matter.ota.apply_action_unknown", slog.Int("action", int(resp.Action)))
		r.resetStateToIdle(otadef.ChangeReasonFailure)
		return false
	}
	if resp.DelayedActionTime > 0 {
		r.updateState(otadef.UpdateStateDelayedOnApply, otadef.ChangeReasonDelayByProvider, nil)
		if r.sleep(ctx, min(time.Duration(resp.DelayedActionTime)*time.Second, otaMaxDelay)) != nil {
			return false
		}
	}
	return true
}

// queryOtaProvider mirrors #queryOtaProvider (:842-957).
func (r *OTASoftwareUpdateRequestor) queryOtaProvider(ctx context.Context, sess OtaProviderSession, loc otadef.ProviderLocation) (queryOutcome, error) {
	info := r.cfg.NodeInformation()
	hw, location := info.HardwareVersion, info.Location
	query := provdef.QueryImageRequest{
		VendorId:           info.VendorID,
		ProductId:          info.ProductID,
		SoftwareVersion:    info.SoftwareVersion,
		ProtocolsSupported: []provdef.DownloadProtocolEnum{provdef.DownloadProtocolBdxSynchronous},
		HardwareVersion:    &hw,
		Location:           &location,
	}
	if r.cfg.CanConsent && !info.LocalConfigDisabled {
		yes := true
		query.RequestorCanConsent = &yes // :855
	}
	details, outcome, err := r.updateAvailableFromProvider(ctx, query, sess, loc)
	if err != nil || details == nil {
		return outcome, err
	}

	now := r.clock.Now()
	if details.userConsentNeeded {
		// :880-899.
		r.updateState(otadef.UpdateStateDelayedOnUserConsent, otadef.ChangeReasonSuccess, nil)
		ok, err := r.requestUserConsent(ctx, details.softwareVersion, details.softwareVersionString)
		if err != nil {
			r.logger.Warn("matter.ota.consent_failed", slog.String("err", err.Error()))
			r.markActiveOtaProviderNoUpdate(ctx, loc)
			r.resetStateToIdle(otadef.ChangeReasonFailure)
			return outcomeFinished, nil
		}
		if !ok {
			r.markActiveOtaProviderNoUpdate(ctx, loc)
			r.resetStateToIdle(otadef.ChangeReasonSuccess)
			return outcomeFinished, nil
		}
	}

	// :902-918: what is left of the provider's delay after the consent.
	if details.delayedActionTime > 0 {
		left := time.Duration(details.delayedActionTime)*time.Second - r.clock.Now().Sub(now)
		if left > 0 {
			r.updateState(otadef.UpdateStateDelayedOnQuery, otadef.ChangeReasonDelayByProvider, nil)
			if err := r.sleep(ctx, min(left, otaMaxDelay)); err != nil {
				return outcomeFinished, err
			}
		}
	}

	// :920-938: download.
	file, err := r.handleDownload(ctx, sess, details.imageURI, details.softwareVersion)
	if err != nil {
		r.logger.Info("matter.ota.download_failed", slog.String("err", err.Error()))
		var de *otaDownloadError
		if errors.As(err, &de) {
			r.emitDownloadErrorEvent(details.softwareVersion, de.bytesTransferred, de.code)
		}
		r.resetStateToIdle(otadef.ChangeReasonFailure)
		if err := os.Remove(r.downloadPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.logger.Warn("matter.ota.partial_delete_failed", slog.String("err", err.Error()))
		}
		return outcomeFinished, nil
	}

	// :940-953.
	if !r.validateApplyUpdate(ctx, provdef.ApplyUpdateRequestRequest{UpdateToken: details.updateToken, NewVersion: details.softwareVersion}, sess, loc) {
		return outcomeFinished, nil
	}
	l := loc
	r.prepareApplyUpdate(ctx, details.softwareVersion, file, &l, details.updateToken)
	return outcomeApplyPrepared, nil
}

func (r *OTASoftwareUpdateRequestor) requestUserConsent(ctx context.Context, version uint32, versionString string) (bool, error) {
	if r.cfg.Applier == nil {
		// :1301-1303, the unimplemented default.
		return false, errors.New("seems 'canConsent' is set, but requestUserConsent() not implemented, declining update")
	}
	return r.cfg.Applier.RequestUserConsent(ctx, version, versionString)
}

// otaDownloadError is OtaDownloadError (:97-107): a platform code and how
// far the download came.
type otaDownloadError struct {
	code             int64
	msg              string
	bytesTransferred uint64
	cause            error
}

func (e *otaDownloadError) Error() string {
	return fmt.Sprintf("OTA Download Error %d: %s", e.code, e.msg)
}
func (e *otaDownloadError) Unwrap() error { return e.cause }

// handleDownload mirrors #handleDownload (:1072-1115) for BDX: Downloading,
// the transfer, the file validated against the offered version, progress
// 100.
func (r *OTASoftwareUpdateRequestor) handleDownload(ctx context.Context, sess OtaProviderSession, uri string, version uint32) (string, error) {
	if !strings.HasPrefix(uri, "bdx://") {
		// HTTPS (:1094-1104) is not ported; any other scheme is refused
		// as matter.js refuses it (:1106).
		return "", fmt.Errorf("unsupported download URI: %s", uri)
	}
	v := version
	r.updateState(otadef.UpdateStateDownloading, otadef.ChangeReasonSuccess, &v)
	fd, _, err := bdx.ParseBdxURI(uri)
	if err != nil {
		return "", err
	}
	if err := r.handleBdxDownload(ctx, sess, fd); err != nil {
		return "", err
	}
	if _, err := r.validateUpdateFile(&version); err != nil {
		return "", err
	}
	full := uint8(100)
	r.setProgress(&full)
	return r.downloadPath(), nil
}

// handleBdxDownload mirrors #handleBdxDownload (:959-1006): up to three
// attempts of a receiver-driven transfer in 1024-byte blocks into the
// download location; the last failure becomes an otaDownloadError with the
// BDX status code, or UpdateState Unknown's value (0) for any other error.
func (r *OTASoftwareUpdateRequestor) handleBdxDownload(ctx context.Context, sess OtaProviderSession, fd bdx.FileDesignator) error {
	for attempt := 1; ; attempt++ {
		zero := uint8(0)
		r.setProgress(&zero)
		err := r.bdxAttempt(ctx, sess, fd)
		if err == nil {
			return nil
		}
		r.logger.Info("matter.ota.bdx_attempt_failed", slog.Int("attempt", attempt), slog.String("err", err.Error()))
		if attempt >= otaBdxDownloadAttempts || ctx.Err() != nil {
			code := int64(otadef.UpdateStateUnknown)
			if be, ok := bdx.AsError(err); ok {
				code = int64(be.Code)
			}
			// matter.js leaves bytesTransferred at 0 here (:995-1002).
			return &otaDownloadError{code: code, msg: fmt.Sprintf("OTA BDX download failed after %d attempts: %v", attempt, err), cause: err}
		}
	}
}

func (r *OTASoftwareUpdateRequestor) bdxAttempt(ctx context.Context, sess OtaProviderSession, fd bdx.FileDesignator) error {
	ex, err := sess.OpenExchange(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = ex.Close() }()
	_, err = bdx.Receive(ctx, bdx.NewMessenger(ex, 0), bdx.ReceiveOptions{
		TransferFileDesignator: fd,
		Sink:                   bdx.NewFileSink(r.downloadPath()),
		PreferredDriverModes:   []bdx.DriverMode{bdx.ReceiverDrive},
		MaxBlockSize:           otaBdxMaxBlockSizeNonTCP,
		Progress:               r.updateProgress,
	})
	return err
}

// updateProgress mirrors #updateProgress (:1064-1070): 0-99 % while
// downloading.
func (r *OTASoftwareUpdateRequestor) updateProgress(downloaded uint64, total *uint64) {
	var p uint8
	if total != nil && *total > 0 {
		p = uint8(downloaded * 99 / *total) //nolint:gosec // at most 99
	} else {
		// :1068, an approximation without a known size.
		p = uint8(99 * (1 - math.Exp(-float64(downloaded)/1_000_000)))
	}
	r.setProgress(&p)
}

func (r *OTASoftwareUpdateRequestor) setProgress(p *uint8) {
	r.mu.Lock()
	same := (r.progress == nil && p == nil) || (r.progress != nil && p != nil && *r.progress == *p)
	if p != nil {
		v := *p
		r.progress = &v
	} else {
		r.progress = nil
	}
	r.mu.Unlock()
	if !same {
		r.changed(otadef.AttrUpdateStateProgress)
	}
}

// validateUpdateFile mirrors validateUpdateFile (:1124-1158): the file's
// header parses, its payload has the stated size, and its version is the
// offered one — or, without one, newer than the running version.
func (r *OTASoftwareUpdateRequestor) validateUpdateFile(newSoftwareVersion *uint32) (otaimage.Header, error) {
	f, err := os.Open(r.downloadPath())
	if err != nil {
		return otaimage.Header{}, &otaDownloadError{code: int64(otadef.UpdateStateUnknown), msg: "Downloaded OTA file is invalid: " + err.Error(), cause: err}
	}
	defer func() { _ = f.Close() }()
	var size uint64
	if fi, err := f.Stat(); err == nil {
		size = uint64(fi.Size()) //nolint:gosec // a file size is not negative
	}
	header, err := otaimage.ValidateFile(f, otaimage.FileOptions{})
	if err == nil {
		switch {
		case newSoftwareVersion == nil:
			if current := r.cfg.NodeInformation().SoftwareVersion; header.SoftwareVersion <= current {
				err = fmt.Errorf("%w: OTA file software version %d not newer than current %d", otaimage.ErrInvalid, header.SoftwareVersion, current)
			}
		case header.SoftwareVersion != *newSoftwareVersion:
			err = fmt.Errorf("%w: OTA file software version %d does not match expected %d", otaimage.ErrInvalid, header.SoftwareVersion, *newSoftwareVersion)
		}
	}
	if err != nil {
		return otaimage.Header{}, &otaDownloadError{code: int64(otadef.UpdateStateUnknown), msg: "Downloaded OTA file is invalid: " + err.Error(), bytesTransferred: size, cause: err}
	}
	return header, nil
}

// updateState mirrors #updateState (:1164-1204): a change emits
// StateTransition; TargetSoftwareVersion is carried only into Downloading,
// Applying and RollingBack; Idle schedules the next query.
func (r *OTASoftwareUpdateRequestor) updateState(newState otadef.UpdateStateEnum, reason otadef.ChangeReasonEnum, target *uint32) {
	carries := newState == otadef.UpdateStateDownloading || newState == otadef.UpdateStateApplying || newState == otadef.UpdateStateRollingBack
	if !carries {
		target = nil
	} else if target == nil {
		panic(fmt.Sprintf("core: OTA requestor: targetSoftwareVersion must be provided for state %d", newState))
	}
	r.mu.Lock()
	previous := r.state
	transition := newState != previous
	if transition {
		r.state = newState
	}
	r.mu.Unlock()
	if transition {
		ev := otadef.StateTransitionEvent{PreviousState: previous, NewState: newState, Reason: reason, TargetSoftwareVersion: spec.Nullable[uint32]{Null: true}}
		if target != nil {
			ev.TargetSoftwareVersion = spec.Nullable[uint32]{Value: *target}
		}
		r.emit(otadef.EventStateTransition, ev)
		r.changed(otadef.AttrUpdateState)
	}
	if newState == otadef.UpdateStateIdle || newState == otadef.UpdateStateUnknown {
		r.scheduleUpdateQuery(nil, reasonNewQuery, nil)
		r.setProgress(nil)
	} else {
		r.mu.Lock()
		reset := r.progress != nil
		r.mu.Unlock()
		if reset {
			zero := uint8(0)
			r.setProgress(&zero)
		}
	}
}

func (r *OTASoftwareUpdateRequestor) resetStateToIdle(reason otadef.ChangeReasonEnum) {
	r.updateState(otadef.UpdateStateIdle, reason, nil)
}

// emitDownloadErrorEvent mirrors #emitDownloadErrorEvent (:1222-1232).
func (r *OTASoftwareUpdateRequestor) emitDownloadErrorEvent(version uint32, bytes uint64, platformCode int64) {
	r.mu.Lock()
	progress := spec.Nullable[uint8]{Null: true}
	if r.progress != nil {
		progress = spec.Nullable[uint8]{Value: *r.progress}
	}
	r.mu.Unlock()
	r.emit(otadef.EventDownloadError, otadef.DownloadErrorEvent{
		SoftwareVersion: version, BytesDownloaded: bytes, ProgressPercent: progress,
		PlatformCode: spec.Nullable[int64]{Value: platformCode},
	})
}

// prepareApplyUpdate mirrors #prepareApplyUpdate (:1240-1260): the update
// is recorded, the state is Applying, and the apply runs once the query
// ends.
func (r *OTASoftwareUpdateRequestor) prepareApplyUpdate(ctx context.Context, version uint32, file string, loc *otadef.ProviderLocation, token []byte) {
	r.mu.Lock()
	r.inProgress = &updateInProgress{NewSoftwareVersion: version, Location: loc, UpdateToken: slices.Clone(token)}
	r.pendingApply = &pendingApply{version: version, file: file}
	r.mu.Unlock()
	r.persist(ctx)
	v := version
	r.updateState(otadef.UpdateStateApplying, otadef.ChangeReasonSuccess, &v)
}

// applyPreparedUpdate mirrors #applyPreparedUpdate (:1262-1285).
func (r *OTASoftwareUpdateRequestor) applyPreparedUpdate(ctx context.Context, version uint32, file string) {
	r.mu.Lock()
	prepared := r.state == otadef.UpdateStateApplying && r.inProgress != nil && r.inProgress.NewSoftwareVersion == version
	r.mu.Unlock()
	if !prepared {
		r.logger.Info("matter.ota.apply_not_prepared", slog.Uint64("version", uint64(version)))
		r.scheduleUpdateQuery(nil, reasonNewQuery, nil)
		return
	}
	var err error
	if r.cfg.Applier == nil {
		err = errors.New("to apply the update the applyUpdate() method must be implemented")
	} else {
		err = r.cfg.Applier.ApplyUpdate(ctx, version, file)
	}
	if err != nil {
		r.logger.Error("matter.ota.apply_failed", slog.Uint64("version", uint64(version)), slog.String("err", err.Error()))
		r.mu.Lock()
		r.inProgress = nil
		r.mu.Unlock()
		r.persist(ctx)
		r.resetStateToIdle(otadef.ChangeReasonFailure)
		return
	}
	r.resetStateToIdle(otadef.ChangeReasonSuccess)
}

// handlePreviousUpdateOnStart mirrors #handlePreviousUpdateOnStart
// (:255-313): running the new version is VersionApplied and Applying → Idle
// (Success), then NotifyUpdateApplied to the provider that offered it; a
// node still on another version reports Applying → Idle (Failure) and asks
// that provider again 2-12 minutes later, like a Busy one.
func (r *OTASoftwareUpdateRequestor) handlePreviousUpdateOnStart(ctx context.Context, d updateInProgress) {
	info := r.cfg.NodeInformation()
	if d.NewSoftwareVersion != info.SoftwareVersion {
		r.logger.Warn("matter.ota.update_not_running", slog.Uint64("expected", uint64(d.NewSoftwareVersion)), slog.Uint64("running", uint64(info.SoftwareVersion)))
		r.emit(otadef.EventStateTransition, otadef.StateTransitionEvent{
			PreviousState: otadef.UpdateStateApplying, NewState: otadef.UpdateStateIdle,
			Reason: otadef.ChangeReasonFailure, TargetSoftwareVersion: spec.Nullable[uint32]{Null: true},
		})
		delay := time.Duration(r.rnd(599)+120) * time.Second
		r.scheduleUpdateQuery(&delay, reasonBusy, d.Location)
		return
	}
	r.emit(otadef.EventVersionApplied, otadef.VersionAppliedEvent{SoftwareVersion: info.SoftwareVersion, ProductId: info.ProductID})
	r.emit(otadef.EventStateTransition, otadef.StateTransitionEvent{
		PreviousState: otadef.UpdateStateApplying, NewState: otadef.UpdateStateIdle,
		Reason: otadef.ChangeReasonSuccess, TargetSoftwareVersion: spec.Nullable[uint32]{Null: true},
	})
	if d.Location == nil || d.UpdateToken == nil {
		return // :292-295, nothing to notify
	}
	sess, err := r.connect(ctx, *d.Location)
	if err == nil {
		_, err = sess.Invoke(ctx, d.Location.Endpoint, provdef.ClusterID, provdef.CmdNotifyUpdateApplied,
			provdef.NotifyUpdateAppliedRequest{SoftwareVersion: d.NewSoftwareVersion, UpdateToken: d.UpdateToken})
	}
	if err != nil {
		// :308-312: the call is not mandatory.
		r.logger.Info("matter.ota.notify_applied_failed", slog.String("err", err.Error()))
		return
	}
	r.logger.Info("matter.ota.notified_applied", slog.Uint64("version", uint64(d.NewSoftwareVersion)))
}
