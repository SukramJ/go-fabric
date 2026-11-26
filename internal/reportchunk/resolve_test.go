// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// badValue is a value whose encoding fails in write: kind "open" leaves a
// list open, "panic" panics.
type badValue string

// writeOrFail is write that also fails a [badValue] the way it names.
func writeOrFail(enc *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
	if b, ok := v.Value.(badValue); ok && !v.IsNull {
		if b == "panic" {
			const p = "encoder bug"
			valueenc.ExpectPanic(p)
			panic(p)
		}
		enc.StartArray(tag)
		enc.PutUint(tlv.AnonymousTag(), 1)
		return
	}
	write(enc, tag, v)
}

// failures collects what a chunker reports as not encodable.
type failures []reportchunk.EncodeFailure

func (f *failures) hook(e reportchunk.EncodeFailure) { *f = append(*f, e) }

// withoutStatuses is rd's attribute and event reports that are not
// statuses.
func withoutStatuses(chunks []im.ReportData) im.ReportData {
	var out im.ReportData
	for _, c := range chunks {
		for _, r := range c.Reports {
			if !r.IsStatus {
				out.Reports = append(out.Reports, r)
			}
		}
		for _, e := range c.EventReports {
			if !e.IsStatus {
				out.EventReports = append(out.EventReports, e)
			}
		}
	}
	return out
}

// TestValueThatCannotBeEncodedAnswersFailure (#625): a value whose
// encoder fails — among good ones, as the first report (the overhead
// measurement), as a list in a report that is split — answers FAILURE on
// its bare path, the hook hears of it once, and the other reports are
// byte for byte what they are without it. Red before #625: Split
// returned the encoder's error (or panicked) and no chunk went out.
func TestValueThatCannotBeEncodedAnswersFailure(t *testing.T) {
	t.Parallel()
	long := make([]uint32, 200)
	for i := range long {
		long[i] = uint32(i)
	}
	good := []im.AttributeReport{attr(1, strings.Repeat("a", 90)), attr(2, long), attr(3, "z")}
	for _, kind := range []badValue{"open", "panic"} {
		for _, at := range []int{0, 1, 3} {
			reps := append(append(append([]im.AttributeReport(nil), good[:at]...), attr(9, kind)), good[at:]...)
			for _, budget := range []int{200, 4000} {
				var got failures
				chunks, err := reportchunk.Split(im.ReportData{Reports: reps}, budget, 1280, writeOrFail, got.hook)
				if err != nil {
					t.Fatalf("%s at %d, budget %d: %v", kind, at, budget, err)
				}
				var status []im.AttributeReport
				for _, c := range chunks {
					for _, r := range c.Reports {
						if r.IsStatus {
							status = append(status, r)
						}
					}
				}
				if len(status) != 1 || status[0].Path.Attribute != 9 || status[0].Status.Status != im.StatusFailure || status[0].Path.HasListIndex || status[0].Path.ListAppend {
					t.Fatalf("%s at %d, budget %d: statuses %+v, want FAILURE for attribute 9 alone", kind, at, budget, status)
				}
				if len(got) != 1 || got[0].IsEvent || got[0].AttributePath.Attribute != 9 || got[0].Err == nil {
					t.Fatalf("%s at %d, budget %d: hook heard %+v", kind, at, budget, got)
				}
				ref, err := reportchunk.Split(im.ReportData{Reports: good}, budget, 1280, write, nil)
				if err != nil {
					t.Fatal(err)
				}
				gotBytes, wantBytes := encodedSizeBytes(t, withoutStatuses(chunks)), encodedSizeBytes(t, withoutStatuses(ref))
				if !bytes.Equal(gotBytes, wantBytes) {
					t.Fatalf("%s at %d, budget %d: the other reports changed:\n% x\nwant\n% x", kind, at, budget, gotBytes, wantBytes)
				}
			}
		}
	}
}

func encodedSizeBytes(t *testing.T, rd im.ReportData) []byte {
	t.Helper()
	b, err := reportchunk.Encode(rd, reportchunk.Writer(write))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFabricScopedValueThatCannotBeEncoded (#625): a fabric-scoped list
// whose encoder fails answers FAILURE for its path, as one that cannot be
// scoped does.
func TestFabricScopedValueThatCannotBeEncoded(t *testing.T) {
	t.Parallel()
	rep := attr(4, badValue("panic"))
	rep.FabricScope = &im.FabricScope{Fabric: 1, Filtered: true}
	var got failures
	chunks, err := reportchunk.Split(im.ReportData{Reports: []im.AttributeReport{rep, attr(1, "x")}}, 1000, 1280, writeOrFail, got.hook)
	if err != nil {
		t.Fatal(err)
	}
	if r := chunks[0].Reports[0]; !r.IsStatus || r.Status.Status != im.StatusFailure || r.FabricScope != nil {
		t.Fatalf("fabric-scoped failing list reported %+v, want FAILURE", r)
	}
	if len(got) != 1 || !errors.Is(got[0].Err, valueenc.ErrPanic) {
		t.Fatalf("hook heard %+v, want the panic", got)
	}
	// ApplyFabricScope alone contains the panic too.
	if r := reportchunk.ApplyFabricScope(rep, writeOrFail); !r.IsStatus || r.Status.Status != im.StatusFailure {
		t.Fatalf("ApplyFabricScope answered %+v, want FAILURE", r)
	}
}

// TestEventPayloadThatCannotBeEncoded (#625): the report-time floor for
// an event — a payload whose encoder fails answers EventStatusIB FAILURE
// for its path; the other event and the attributes are delivered.
func TestEventPayloadThatCannotBeEncoded(t *testing.T) {
	t.Parallel()
	ev := func(id uint32, v any) im.EventReport {
		return im.EventReport{
			Path:   im.ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: id, HasEndpoint: true, HasCluster: true, HasEvent: true},
			Number: uint64(id), Priority: im.EventPriorityInfo, Data: im.AttributeValue{Value: v},
		}
	}
	var got failures
	c := reportchunk.New(im.ReportData{}, 1000, 1280, writeOrFail, reportchunk.Slice([]im.AttributeReport{attr(1, "x")}),
		func() []im.EventReport { return []im.EventReport{ev(1, "a"), ev(2, badValue("open")), ev(3, "c")} }, got.hook)
	chunks := drain(t, c)
	evs := chunks[0].EventReports
	if len(chunks) != 1 || len(chunks[0].Reports) != 1 || len(evs) != 3 {
		t.Fatalf("chunks %+v", chunks)
	}
	if !evs[1].IsStatus || evs[1].Status.Status != im.StatusFailure || evs[1].Path.Event != 2 || evs[0].IsStatus || evs[2].IsStatus {
		t.Fatalf("event reports %+v, want FAILURE for event 2 alone", evs)
	}
	if len(got) != 1 || !got[0].IsEvent || got[0].EventPath.Event != 2 {
		t.Fatalf("hook heard %+v", got)
	}
}

// TestChunkerRunsTheWriterOncePerValue (#625): the chunker resolves every
// value when it admits the report, and the size measurements and the
// final encode write those bytes back — the writer runs exactly once per
// attribute value and event payload, also in a report that chunks.
func TestChunkerRunsTheWriterOncePerValue(t *testing.T) {
	t.Parallel()
	const n = 500
	calls := 0
	c := reportchunk.New(im.ReportData{HasSubscription: true, SubscriptionID: 1}, 1000, 1280, countingWrite(&calls),
		reportchunk.Slice(smallReports(n)), func() []im.EventReport { return smallEvents(n) }, nil)
	chunks := drain(t, c)
	if len(chunks) < 2 {
		t.Fatalf("%d chunks; the test needs several", len(chunks))
	}
	for _, chunk := range chunks {
		if _, err := reportchunk.Encode(chunk, reportchunk.Writer(countingWrite(&calls))); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2*n {
		t.Fatalf("%d value writes for %d values, want exactly one each", calls, 2*n)
	}
}

// TestResolveInvoke (#625): a command response whose encoder fails is
// answered FAILURE on the request's command path with its CommandRef; a
// good one is carried as its bytes and written back verbatim; statuses
// and the caller's response are untouched.
func TestResolveInvoke(t *testing.T) {
	t.Parallel()
	fieldsWrite := func(enc *tlv.Encoder, tag tlv.Tag, fields any) {
		writeOrFail(enc, tag, im.AttributeValue{Value: fields})
	}
	path := func(cmd uint32) im.ConcreteCommandPath {
		return im.ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: cmd, HasEndpoint: true, HasCluster: true, HasCommand: true}
	}
	req := im.InvokeRequest{Invokes: []im.CommandInvocation{
		{Path: path(1), CommandRef: 1, HasCommandRef: true},
		{Path: path(2), CommandRef: 2, HasCommandRef: true},
		{Path: path(3), CommandRef: 3, HasCommandRef: true},
	}}
	resp := im.InvokeResponse{Responses: []im.InvokeResponseEntry{
		{Path: path(0x81), Response: badValue("panic"), HasResponse: true, CommandRef: 1, HasCommandRef: true},
		{Path: path(0x82), Response: "ok", HasResponse: true, CommandRef: 2, HasCommandRef: true},
		{Path: path(3), IsStatus: true, Status: im.StatusIB{Status: im.StatusSuccess}, CommandRef: 3, HasCommandRef: true},
	}}
	var heard []reportchunk.InvokeFailure
	got := reportchunk.ResolveInvoke(req, resp, fieldsWrite, func(f reportchunk.InvokeFailure) { heard = append(heard, f) })
	if bad := got.Responses[0]; !bad.IsStatus || bad.Status.Status != im.StatusFailure || bad.Path != path(1) || bad.CommandRef != 1 || !bad.HasCommandRef {
		t.Fatalf("failing response %+v, want FAILURE on command 1 with CommandRef 1", bad)
	}
	if _, ok := got.Responses[1].Response.(reportchunk.Encoded); !ok {
		t.Fatalf("good response carried as %T, want its bytes", got.Responses[1].Response)
	}
	if got.Responses[2] != resp.Responses[2] || resp.Responses[0].IsStatus {
		t.Fatal("a status entry changed, or the caller's response was modified")
	}
	if len(heard) != 1 || heard[0].Path != path(1) {
		t.Fatalf("hook heard %+v", heard)
	}
	var a, b []byte
	encA, encB := tlv.NewEncoder(), tlv.NewEncoder()
	reportchunk.CommandWriter(fieldsWrite)(encA, tlv.ContextTag(1), got.Responses[1].Response)
	fieldsWrite(encB, tlv.ContextTag(1), "ok")
	a, _ = encA.Bytes()
	b, _ = encB.Bytes()
	if !bytes.Equal(a, b) {
		t.Fatalf("resolved response writes % x, want % x", a, b)
	}
	// The request's only invocation names an entry without a CommandRef.
	single := reportchunk.ResolveInvoke(im.InvokeRequest{Invokes: req.Invokes[:1]},
		im.InvokeResponse{Responses: []im.InvokeResponseEntry{{Path: path(0x81), Response: badValue("open"), HasResponse: true}}}, fieldsWrite, nil)
	if r := single.Responses[0]; !r.IsStatus || r.Path != path(1) {
		t.Fatalf("single failing response %+v, want FAILURE on command 1", r)
	}
	// An entry without a CommandRef in a request of several is matched by
	// its cluster and endpoint when one invocation alone has them.
	other := im.CommandInvocation{Path: im.ConcreteCommandPath{Endpoint: 2, Cluster: 8, Command: 4, HasEndpoint: true, HasCluster: true, HasCommand: true}}
	byPath := reportchunk.ResolveInvoke(im.InvokeRequest{Invokes: []im.CommandInvocation{{Path: path(1)}, other}},
		im.InvokeResponse{Responses: []im.InvokeResponseEntry{{Path: path(0x81), Response: badValue("open"), HasResponse: true}}}, fieldsWrite, nil)
	if r := byPath.Responses[0]; !r.IsStatus || r.Path != path(1) {
		t.Fatalf("failing response matched by path %+v, want FAILURE on command 1", r)
	}
}

// TestSplitFastPathEventThatCannotBeEncoded (#625, review L4): the ongoing
// report path (Split, a report that fits whole) resolves its events too —
// a payload that cannot be encoded answers EventStatusIB FAILURE, the
// other event and the attribute are delivered, the hook hears of it once.
func TestSplitFastPathEventThatCannotBeEncoded(t *testing.T) {
	t.Parallel()
	ev := func(id uint32, v any) im.EventReport {
		return im.EventReport{
			Path:   im.ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, Event: id, HasEndpoint: true, HasCluster: true, HasEvent: true},
			Number: uint64(id), Priority: im.EventPriorityInfo, Data: im.AttributeValue{Value: v},
		}
	}
	for _, kind := range []badValue{"open", "panic"} {
		var got failures
		rd := im.ReportData{Reports: []im.AttributeReport{attr(1, "x")}, EventReports: []im.EventReport{ev(1, "a"), ev(2, kind)}}
		chunks, err := reportchunk.Split(rd, 4000, 4000, writeOrFail, got.hook)
		if err != nil || len(chunks) != 1 {
			t.Fatalf("%s: %d chunks, %v; want the report whole", kind, len(chunks), err)
		}
		evs := chunks[0].EventReports
		if len(chunks[0].Reports) != 1 || len(evs) != 2 || evs[0].IsStatus || !evs[1].IsStatus || evs[1].Status.Status != im.StatusFailure {
			t.Fatalf("%s: chunk %+v, want event 2 FAILURE and the rest delivered", kind, chunks[0])
		}
		if len(got) != 1 || !got[0].IsEvent || got[0].EventPath.Event != 2 {
			t.Fatalf("%s: hook heard %+v", kind, got)
		}
		if rd.EventReports[1].IsStatus {
			t.Fatalf("%s: the caller's report was modified", kind)
		}
	}
}
