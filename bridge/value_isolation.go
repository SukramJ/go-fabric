// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"log/slog"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// encodeValueIsolated is v as [defaultAttributeValueWriter] writes it, one
// anonymous element, written on an encoder of its own under a recover
// ([valueenc.Encode]): a value whose encoder panics, leaves a container
// open or writes other than one element is an error, never a broken
// message (#625).
func encodeValueIsolated(v im.AttributeValue) ([]byte, error) {
	return valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) {
		defaultAttributeValueWriter(enc, tag, v)
	})
}

// reportEncodeFailureLogger is the chunker's hook for a value it could not
// encode ([reportchunk.EncodeFailure]): the path answers FAILURE, and the
// log names it at ERROR — the controller sees only the status
// ([logEncodeFailure]). A nil logger is no hook.
func reportEncodeFailureLogger(logger *slog.Logger) func(reportchunk.EncodeFailure) {
	if logger == nil {
		return nil
	}
	return func(f reportchunk.EncodeFailure) {
		if f.IsEvent {
			logEncodeFailure(logger, "matter.event.encode_failed", f.Err,
				slog.Int("endpoint", int(f.EventPath.Endpoint)),
				slog.Uint64("cluster", uint64(f.EventPath.Cluster)),
				slog.Uint64("event", uint64(f.EventPath.Event)),
				slog.String("status", im.StatusFailure.String()))
			return
		}
		logEncodeFailure(logger, "matter.attribute.encode_failed", f.Err,
			slog.Int("endpoint", int(f.AttributePath.Endpoint)),
			slog.Uint64("cluster", uint64(f.AttributePath.Cluster)),
			slog.Uint64("attribute", uint64(f.AttributePath.Attribute)),
			slog.String("status", im.StatusFailure.String()))
	}
}

// invokeEncodeFailureLogger is [reportchunk.ResolveInvoke]'s hook for a
// command response it could not encode: the command answers FAILURE, and
// the log names it at ERROR ([logEncodeFailure]). A nil logger is no hook.
func invokeEncodeFailureLogger(logger *slog.Logger) func(reportchunk.InvokeFailure) {
	if logger == nil {
		return nil
	}
	return func(f reportchunk.InvokeFailure) {
		logEncodeFailure(logger, "matter.command.encode_failed", f.Err,
			slog.Int("endpoint", int(f.Path.Endpoint)),
			slog.Uint64("cluster", uint64(f.Path.Cluster)),
			slog.Uint64("command", uint64(f.Path.Command)),
			slog.String("status", im.StatusFailure.String()))
	}
}

// logEncodeFailure logs a value that could not be encoded: the path and the
// error at ERROR, once per occurrence, and — for a panic — its stack at
// DEBUG, so a value that fails on every Read does not put a stack into the
// log at the default level. The error names a panic value's type, never
// the value ([valueenc.PanicError]: a host encoder's panic may carry what
// it was writing, which log_redact would have redacted).
func logEncodeFailure(logger *slog.Logger, msg string, err error, path ...slog.Attr) {
	args := make([]any, 0, len(path)+1)
	for _, a := range path {
		args = append(args, a)
	}
	logger.Error(msg, append(args, slog.Any("err", err))...)
	if stack := valueenc.PanicStack(err); stack != nil {
		logger.Debug(msg, append(args[:len(path):len(path)], slog.String("stack", string(stack)))...)
	}
}
