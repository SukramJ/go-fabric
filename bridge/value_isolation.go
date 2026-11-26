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
// log names it at ERROR — the controller sees only the status. A nil
// logger is no hook.
func reportEncodeFailureLogger(logger *slog.Logger) func(reportchunk.EncodeFailure) {
	if logger == nil {
		return nil
	}
	return func(f reportchunk.EncodeFailure) {
		if f.IsEvent {
			logger.Error("matter.event.encode_failed",
				slog.Int("endpoint", int(f.EventPath.Endpoint)),
				slog.Uint64("cluster", uint64(f.EventPath.Cluster)),
				slog.Uint64("event", uint64(f.EventPath.Event)),
				slog.String("answered", "EventStatusIB FAILURE"),
				slog.Any("err", f.Err))
			return
		}
		logger.Error("matter.attribute.encode_failed",
			slog.Int("endpoint", int(f.AttributePath.Endpoint)),
			slog.Uint64("cluster", uint64(f.AttributePath.Cluster)),
			slog.Uint64("attribute", uint64(f.AttributePath.Attribute)),
			slog.String("answered", "AttributeStatusIB FAILURE"),
			slog.Any("err", f.Err))
	}
}

// invokeEncodeFailureLogger is [reportchunk.ResolveInvoke]'s hook for a
// command response it could not encode: the command answers FAILURE, and
// the log names it at ERROR. A nil logger is no hook.
func invokeEncodeFailureLogger(logger *slog.Logger) func(reportchunk.InvokeFailure) {
	if logger == nil {
		return nil
	}
	return func(f reportchunk.InvokeFailure) {
		logger.Error("matter.command.encode_failed",
			slog.Int("endpoint", int(f.Path.Endpoint)),
			slog.Uint64("cluster", uint64(f.Path.Cluster)),
			slog.Uint64("command", uint64(f.Path.Command)),
			slog.String("answered", "CommandStatusIB FAILURE"),
			slog.Any("err", f.Err))
	}
}
