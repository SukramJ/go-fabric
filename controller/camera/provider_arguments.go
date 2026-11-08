// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/tlv"
)

// providerCommandName is a WebRTCTransportProvider command a client may
// send through the controller.
type providerCommandName string

// The provider commands a client may send. EndSession is absent on
// purpose: stopping a stream owns it, so the session records end with it.
// Mirrors matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:18-32
// at 0bd06f2.
const (
	cmdProvideOffer         providerCommandName = "ProvideOffer"
	cmdSolicitOffer         providerCommandName = "SolicitOffer"
	cmdProvideAnswer        providerCommandName = "ProvideAnswer"
	cmdProvideIceCandidates providerCommandName = "ProvideIceCandidates"
)

var providerCommandNames = []providerCommandName{cmdProvideOffer, cmdSolicitOffer, cmdProvideAnswer, cmdProvideIceCandidates}

// isProviderCommandName reports whether name is one of the provider
// commands a client may send.
func isProviderCommandName(name string) bool {
	return slices.Contains(providerCommandNames, providerCommandName(name))
}

// establishesWebRTCSession reports whether a command opens a session, which
// needs the originating endpoint injected, stream fields reconciled and the
// session registered with the local requestor. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:34-50 at
// 0bd06f2.
func establishesWebRTCSession(name providerCommandName) bool {
	return name == cmdProvideOffer || name == cmdSolicitOffer
}

// wireValue is a converted field value, written as TLV under its tag.
type wireValue interface {
	putTLV(enc *tlv.Encoder, tag tlv.Tag)
}

type (
	uintValue   uint64
	stringValue string
	boolValue   bool
	nullValue   struct{}
	listValue   []wireValue
	// structValue is a struct's members by field id, in model order.
	structValue []memberValue
	// encodedValue is a value a generated type encodes itself.
	encodedValue struct{ v spec.Encodable }
)

// memberValue is one member of a structValue.
type memberValue struct {
	id    uint32
	value wireValue
}

func (v uintValue) putTLV(enc *tlv.Encoder, tag tlv.Tag)   { enc.PutUint(tag, uint64(v)) }
func (v stringValue) putTLV(enc *tlv.Encoder, tag tlv.Tag) { enc.PutUTF8(tag, string(v)) }
func (v boolValue) putTLV(enc *tlv.Encoder, tag tlv.Tag)   { enc.PutBool(tag, bool(v)) }
func (nullValue) putTLV(enc *tlv.Encoder, tag tlv.Tag)     { enc.PutNull(tag) }
func (v encodedValue) putTLV(enc *tlv.Encoder, tag tlv.Tag) {
	v.v.EncodeTLV(enc, tag)
}

func (v listValue) putTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartArray(tag)
	for _, e := range v {
		e.putTLV(enc, tlv.AnonymousTag())
	}
	_ = enc.EndContainer()
}

func (v structValue) putTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	for _, m := range v {
		m.value.putTLV(enc, tlv.ContextTag(uint8(m.id))) //nolint:gosec // G115: field ids of the generated model fit a context tag
	}
	_ = enc.EndContainer()
}

// fieldConverter converts a caller's JSON value for one field, refusing a
// bad value with an argumentError. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:52-53 at
// 0bd06f2.
type fieldConverter func(value json.RawMessage, field string) (wireValue, error)

var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)

// canonicalKey drops case and separators, so webrtc_session_id matches
// webRtcSessionId and sdpMLineIndex matches sdpmLineIndex. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:55-61
// at 0bd06f2.
func canonicalKey(key string) string {
	return strings.ToLower(nonAlphanumeric.ReplaceAllString(key, ""))
}

// propertyName is a model field's name as matter.js spells the property:
// the generated name with its first letter lower-cased.
func propertyName(f *spec.Field) string {
	if f.Name == "" {
		return ""
	}
	return strings.ToLower(f.Name[:1]) + f.Name[1:]
}

// iceServerKeys are the keys of a wire ICE server, matched exactly. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:63-70
// at 0bd06f2.
var iceServerKeys = []string{"urls", "username", "credential", "caid"}

// rejectUnknownKeys refuses every key a caller states that is not known:
// dropping an unknown one would answer a different request. Mirrors
// matterjs-server packages/ws-controller/src/camera/wireArgumentChecks.ts:15-23
// at 0bd06f2.
func rejectUnknownKeys(members []jsonMember, known []string, subject string) error {
	var unknown []string
	for _, m := range members {
		if !slices.Contains(known, m.key) {
			unknown = append(unknown, m.key)
		}
	}
	if len(unknown) > 0 {
		return invalidArguments("unknown %s key: %s. Accepted: %s", subject, strings.Join(unknown, ", "), strings.Join(known, ", "))
	}
	return nil
}

// toIceServer converts a wire ICE server (W3C RTCIceServer: urls is one URL
// or a list) into the ICEServerStruct (urls always a list, §11.4.5.3).
// Mirrors matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:72-100
// at 0bd06f2.
func toIceServer(value json.RawMessage, field string) (wpdef.ICEServerStruct, error) {
	var out wpdef.ICEServerStruct
	members, ok := jsonObject(value)
	if !ok {
		return out, invalidArguments("%s must be an object", field)
	}
	if err := rejectUnknownKeys(members, iceServerKeys, field); err != nil {
		return out, err
	}
	get := func(key string) (json.RawMessage, bool) {
		for _, m := range members {
			if m.key == key {
				return m.value, true
			}
		}
		return nil, false
	}
	var urls []json.RawMessage
	if raw, ok := get("urls"); ok {
		if list, isList := jsonArray(raw); isList {
			urls = list
		} else {
			urls = []json.RawMessage{raw}
		}
	}
	if len(urls) == 0 || len(urls) > iceServerLimits.maxUrls {
		return out, invalidArguments("%s.urls must name between 1 and %d servers", field, iceServerLimits.maxUrls)
	}
	out.Urls = make([]string, len(urls))
	for i, raw := range urls {
		url, err := toBoundedString(raw, fmt.Sprintf("%s.urls[%d]", field, i), iceServerLimits.maxURLLength)
		if err != nil {
			return out, err
		}
		out.Urls[i] = url
	}
	if raw, ok := get("username"); ok {
		s, err := toBoundedString(raw, field+".username", iceServerLimits.maxUsernameLength)
		if err != nil {
			return out, err
		}
		out.Username = &s
	}
	if raw, ok := get("credential"); ok {
		s, err := toBoundedString(raw, field+".credential", iceServerLimits.maxCredentialLength)
		if err != nil {
			return out, err
		}
		out.Credential = &s
	}
	if raw, ok := get("caid"); ok {
		n, err := toRequiredNumber(raw, field+".caid", iceServerLimits.caid)
		if err != nil {
			return out, err
		}
		caid := uint16(n) //nolint:gosec // G115: held to the Caid range, at most 65534
		out.Caid = &caid
	}
	return out, nil
}

// toIceServers converts a wire ICE server list; the refusal text is the one
// the list converter gives the same list. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:102-108 at
// 0bd06f2.
func toIceServers(value json.RawMessage, field string) ([]wpdef.ICEServerStruct, error) {
	entries, ok := jsonArray(value)
	if !ok || len(entries) > iceServerLimits.maxServers {
		return nil, invalidArguments("%s must be an array of 0 to %d entries", field, iceServerLimits.maxServers)
	}
	out := make([]wpdef.ICEServerStruct, len(entries))
	for i, e := range entries {
		s, err := toIceServer(e, fmt.Sprintf("%s[%d]", field, i))
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// The structs the provider commands carry; a struct without a converter
// panics in converterFor when the contracts are built. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:110-117
// at 0bd06f2.
const (
	iceServerStruct    = "WebRtcTransportDefinitions.ICEServerStruct"
	iceCandidateStruct = "WebRtcTransportDefinitions.ICECandidateStruct"
)

// originatingEndpointID is controller-owned: the session layer injects the
// requestor's own endpoint over any value a caller states. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:119-120
// at 0bd06f2.
var originatingEndpointID = canonicalKey("originatingEndpointId")

// stringConverter holds a string to the length its field states. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:122-137
// at 0bd06f2.
func stringConverter(f *spec.Field) fieldConverter {
	if b := f.Constraint.Max; b.IsInt() {
		return func(value json.RawMessage, name string) (wireValue, error) {
			s, err := toBoundedString(value, name, int(b.Int))
			return stringValue(s), err
		}
	}
	if b := f.Constraint.Min; b.IsInt() {
		return func(value json.RawMessage, name string) (wireValue, error) {
			var s string
			if jsonKind(value) != '"' || json.Unmarshal(value, &s) != nil || int64(jsLength(s)) < b.Int {
				return nil, invalidArguments("%s must be a string of at least %d characters", name, b.Int)
			}
			return stringValue(s), nil
		}
	}
	return func(value json.RawMessage, name string) (wireValue, error) {
		var s string
		if jsonKind(value) != '"' || json.Unmarshal(value, &s) != nil {
			return nil, invalidArguments("%s must be a string", name)
		}
		return stringValue(s), nil
	}
}

// listConverter holds a list to its length and converts every entry by the
// entry's type. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:139-160 at
// 0bd06f2.
func listConverter(f *spec.Field) fieldConverter {
	if f.Type.Entry == nil {
		panic("camera: the Matter model states no entry type for " + f.Name)
	}
	convertEntry := converterFor(f.Type.Entry)
	minLength, maxLength := 0, -1
	if b := f.Constraint.Min; b.IsInt() {
		minLength = int(b.Int)
	}
	if b := f.Constraint.Max; b.IsInt() {
		maxLength = int(b.Int)
	}
	expected := fmt.Sprintf("an array of %d or more entries", minLength)
	if maxLength >= 0 {
		expected = fmt.Sprintf("an array of %d to %d entries", minLength, maxLength)
	}
	return func(value json.RawMessage, name string) (wireValue, error) {
		entries, ok := jsonArray(value)
		if !ok || len(entries) < minLength || (maxLength >= 0 && len(entries) > maxLength) {
			return nil, invalidArguments("%s must be %s", name, expected)
		}
		out := make(listValue, len(entries))
		for i, e := range entries {
			v, err := convertEntry(e, fmt.Sprintf("%s[%d]", name, i))
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
}

// converterFor is the converter of a model field; a field it cannot
// describe panics, which stops the package from loading. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:162-186
// at 0bd06f2.
func converterFor(f *spec.Field) fieldConverter {
	switch f.Type.Kind {
	case spec.KindStruct:
		switch f.Type.Name {
		case iceServerStruct:
			return func(value json.RawMessage, name string) (wireValue, error) {
				s, err := toIceServer(value, name)
				return encodedValue{s}, err
			}
		case iceCandidateStruct:
			return structConverter(f)
		default:
			panic(fmt.Sprintf("camera: %s carries the struct %s; name a converter for it", f.Name, f.Type.Name))
		}
	case spec.KindList:
		return listConverter(f)
	case spec.KindString:
		return stringConverter(f)
	case spec.KindBool:
		return func(value json.RawMessage, name string) (wireValue, error) {
			var b bool
			if jsonKind(value) != 't' || json.Unmarshal(value, &b) != nil {
				return nil, invalidArguments("%s must be a boolean", name)
			}
			return boolValue(b), nil
		}
	default:
		r := fieldRangeOf(f)
		return func(value json.RawMessage, name string) (wireValue, error) {
			n, err := toRequiredNumber(value, name, r)
			return uintValue(uint64(n)), err //nolint:gosec // G115: held to a range of an unsigned type
		}
	}
}

// fieldContract is what the boundary owes one field. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:188-195
// at 0bd06f2.
type fieldContract struct {
	id        uint32
	property  string
	key       string
	convert   fieldConverter
	mandatory bool
	nullable  bool
	// nullGates are the canonical keys of the fields whose null this
	// field's conformance is conditioned on.
	nullGates []string
}

// nullComparedNames collects the names a conformance compares to null:
// ProvideOffer.StreamUsage is "WebRTCSessionID == null, O" (§11.5.6.3). It
// does not descend into ! or |, where the meaning would invert or vanish.
// Mirrors matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:197-226
// at 0bd06f2.
func nullComparedNames(c spec.Conformance, into *[]string) {
	switch c.Op {
	case spec.ConfOtherwise:
		for _, clause := range c.Args {
			nullComparedNames(clause, into)
		}
	case spec.ConfChoice, spec.ConfOptionalIf:
		if len(c.Args) > 0 {
			nullComparedNames(c.Args[0], into)
		}
	case spec.ConfAnd:
		for _, side := range c.Args {
			nullComparedNames(side, into)
		}
	case spec.ConfEq:
		if len(c.Args) == 2 && c.Args[0].Op == spec.ConfName && c.Args[1].Op == spec.ConfValue && c.Args[1].Value == "null" {
			if key := canonicalKey(c.Args[0].Name); !slices.Contains(*into, key) {
				*into = append(*into, key)
			}
		}
	default:
	}
}

// isMandatory is matter.js's Conformance.isMandatory: M, or an otherwise
// list whose first M comes before any P (packages/model/src/aspects/Conformance.ts:84-100
// of matter.js at 862ad75f).
func isMandatory(c spec.Conformance) bool {
	switch c.Op {
	case spec.ConfMandatory:
		return true
	case spec.ConfOtherwise:
		for _, clause := range c.Args {
			switch clause.Op {
			case spec.ConfProvisional:
				return false
			case spec.ConfMandatory:
				return true
			default:
			}
		}
	default:
	}
	return false
}

// fieldsContract is the contract of a command or struct: the fields by
// canonical key in model order, the mandatory and accepted properties, and
// the one key dropped from a payload rather than refused. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:228-235
// at 0bd06f2.
type fieldsContract struct {
	fields    []*fieldContract
	byKey     map[string]*fieldContract
	mandatory []string
	accepted  []string
	dropped   string
}

// contractFor builds the contract of fields, panicking when two fields
// share a canonical key. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:237-269 at
// 0bd06f2.
func contractFor(fields []spec.Field, subject, skip string) *fieldsContract {
	c := &fieldsContract{byKey: map[string]*fieldContract{}}
	for i := range fields {
		f := &fields[i]
		property := propertyName(f)
		key := canonicalKey(property)
		if key == skip {
			c.dropped = key
			continue
		}
		if clash, ok := c.byKey[key]; ok {
			panic(fmt.Sprintf("camera: %s states %s and %s, which no wire key can tell apart", subject, property, clash.property))
		}
		fc := &fieldContract{
			id:        f.ID,
			property:  property,
			key:       key,
			convert:   converterFor(f),
			mandatory: isMandatory(f.Conformance),
			nullable:  f.Quality.Nullable,
		}
		nullComparedNames(f.Conformance, &fc.nullGates)
		c.fields = append(c.fields, fc)
		c.byKey[key] = fc
		c.accepted = append(c.accepted, property)
		if fc.mandatory {
			c.mandatory = append(c.mandatory, property)
		}
	}
	return c
}

// structConverter accepts a struct spelled the way this API's events emit
// it. Mirrors matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:271-278
// at 0bd06f2.
func structConverter(f *spec.Field) fieldConverter {
	contract := contractFor(f.Type.Struct.Fields, f.Type.Name, "")
	return func(value json.RawMessage, name string) (wireValue, error) {
		members, ok := jsonObject(value)
		if !ok {
			return nil, invalidArguments("%s must be an object", name)
		}
		got, err := contract.toFields(members, name, name)
		if err != nil {
			return nil, err
		}
		return got.structValue(), nil
	}
}

// convertedFields are a payload's fields after the contract: by property,
// in the order they were stated; a nil value is an explicit null.
type convertedFields struct {
	contract *fieldsContract
	values   map[string]wireValue
}

// has reports whether the payload states property.
func (c *convertedFields) has(property string) bool {
	_, ok := c.values[property]
	return ok
}

// structValue is the fields as a struct in model order.
func (c *convertedFields) structValue() structValue {
	var out structValue
	for _, f := range c.contract.fields {
		if v, ok := c.values[f.property]; ok {
			out = append(out, memberValue{id: f.id, value: v})
		}
	}
	return out
}

// toFields converts a payload by the contract; keyPrefix "" names a field
// by its key alone. Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:280-316 at
// 0bd06f2.
func (c *fieldsContract) toFields(members []jsonMember, subject, keyPrefix string) (*convertedFields, error) {
	out := &convertedFields{contract: c, values: map[string]wireValue{}}
	// Includes keys whose null was dropped, so a duplicate is caught even
	// then.
	statedKeys := map[string]bool{}
	for _, m := range members {
		canonical := canonicalKey(m.key)
		if canonical == c.dropped && c.dropped != "" {
			continue
		}
		f, ok := c.byKey[canonical]
		if !ok {
			return nil, invalidArguments("unknown %s key: %s. Keys are matched with case and word separators ignored. Accepted: %s",
				subject, m.key, strings.Join(c.accepted, ", "))
		}
		if statedKeys[canonical] {
			return nil, invalidArguments("%s states %s twice, last as %s", subject, f.property, m.key)
		}
		statedKeys[canonical] = true
		named := m.key
		if keyPrefix != "" {
			named = keyPrefix + "." + m.key
		}
		if jsonKind(m.value) == 'n' {
			// null on a non-nullable optional field means "unset".
			if !f.mandatory && !f.nullable {
				continue
			}
			if !f.nullable {
				return nil, invalidArguments("%s must not be null", named)
			}
			out.values[f.property] = nullValue{}
			continue
		}
		v, err := f.convert(m.value, named)
		if err != nil {
			return nil, err
		}
		out.values[f.property] = v
	}
	for _, property := range c.mandatory {
		if !out.has(property) {
			return nil, invalidArguments("%s requires %s", subject, property)
		}
	}
	return out, nil
}

// requestFields are a provider command's request fields from the
// generated model.
func requestFields(name providerCommandName) []spec.Field {
	return commandFields(wpdef.Definition, string(name))
}

// providerContracts are built when the package loads, so a Matter model
// this boundary cannot describe stops the program at start. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:318-329
// at 0bd06f2.
var providerContracts = func() map[providerCommandName]*fieldsContract {
	contracts := make(map[providerCommandName]*fieldsContract, len(providerCommandNames))
	for _, name := range providerCommandNames {
		contracts[name] = contractFor(requestFields(name), string(name), originatingEndpointID)
	}
	return contracts
}()

// providerFields is a client's provider command after the boundary: the
// converted fields and the notices matterjs-server logs as warnings, for
// the caller to log.
type providerFields struct {
	name    providerCommandName
	fields  *convertedFields
	notices []string
}

// toProviderCommandFields converts a client's provider command payload (a
// JSON object, keys matched with case and word separators ignored) by the
// command's model. target names the device in notices; subject names what
// a refusal is about ("" for "<command> payload"). Mirrors matterjs-server
// packages/ws-controller/src/camera/webRtcProviderArguments.ts:331-344 at
// 0bd06f2.
func toProviderCommandFields(name providerCommandName, payload json.RawMessage, target, subject string) (*providerFields, error) {
	if subject == "" {
		subject = string(name) + " payload"
	}
	members, ok := jsonObject(payload)
	if !ok {
		return nil, invalidArguments("%s must be an object", subject)
	}
	contract := providerContracts[name]
	fields, err := contract.toFields(members, subject, "")
	if err != nil {
		return nil, err
	}
	return &providerFields{name: name, fields: fields, notices: fieldsPastTheirGate(name, fields, target)}, nil
}

// fieldsPastTheirGate words a notice for each field the cluster describes
// for a new session only (§11.5.6.3) that a request stating its gate
// carries. They are forwarded, not refused: the camera decides. Mirrors
// matterjs-server packages/ws-controller/src/camera/webRtcProviderArguments.ts:346-373
// at 0bd06f2.
func fieldsPastTheirGate(name providerCommandName, fields *convertedFields, target string) []string {
	var notices []string
	for _, gate := range fields.contract.fields {
		gateValue, ok := fields.values[gate.property]
		if !ok || gateValue == (nullValue{}) {
			continue
		}
		var statedFields []string
		for _, f := range fields.contract.fields {
			if slices.Contains(f.nullGates, gate.key) && fields.has(f.property) {
				statedFields = append(statedFields, f.property)
			}
		}
		if len(statedFields) == 0 {
			continue
		}
		them := "them"
		if len(statedFields) == 1 {
			them = "it"
		}
		forTarget := ""
		if target != "" {
			forTarget = " for " + target
		}
		notices = append(notices, fmt.Sprintf(
			"%s%s states %s while %s is %s. The cluster describes %s for a request whose %s is null, so the camera may ignore %s; this boundary does not change %s.",
			name, forTarget, strings.Join(statedFields, ", "), gate.property, wireText(gateValue), them, gate.property, them, them))
	}
	return notices
}

// wireText is String(value) of a gate's value, which is a number or a
// boolean.
func wireText(v wireValue) string {
	switch v := v.(type) {
	case uintValue:
		return strconv.FormatUint(uint64(v), 10)
	case boolValue:
		return strconv.FormatBool(bool(v))
	case stringValue:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// request is the command's request, its originating endpoint set to
// endpoint for a command that establishes a session: the field is
// controller-owned, so the session layer states its own requestor endpoint
// over any value the caller sent, as matterjs-server does in
// packages/ws-controller/src/controller/webRtcSessionStreams.ts:237 at
// 0bd06f2.
func (p *providerFields) request(endpoint uint16) (spec.Encodable, error) {
	values := p.fields.structValue()
	if establishesWebRTCSession(p.name) {
		id := requireField(requestFields(p.name), string(p.name), "OriginatingEndpointId").ID
		values = append(values, memberValue{id: id, value: uintValue(endpoint)})
		slices.SortStableFunc(values, func(a, b memberValue) int { return int(a.id) - int(b.id) })
	}
	enc := tlv.NewEncoder()
	values.putTLV(enc, tlv.AnonymousTag())
	raw, err := enc.Bytes()
	if err != nil {
		return nil, fmt.Errorf("camera: encode %s: %w", p.name, err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(raw))
	if err != nil {
		return nil, fmt.Errorf("camera: decode %s: %w", p.name, err)
	}
	switch p.name {
	case cmdProvideOffer:
		return decodeRequest[wpdef.ProvideOfferRequest](n)
	case cmdSolicitOffer:
		return decodeRequest[wpdef.SolicitOfferRequest](n)
	case cmdProvideAnswer:
		return decodeRequest[wpdef.ProvideAnswerRequest](n)
	default:
		return decodeRequest[wpdef.ProvideIceCandidatesRequest](n)
	}
}

// decodeRequest decodes a request the contract has already validated.
func decodeRequest[T spec.Encodable, P interface {
	*T
	DecodeTLV(spec.Node) error
}](n spec.Node) (spec.Encodable, error) {
	var v T
	if err := P(&v).DecodeTLV(n); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return v, nil
}

// ParseICECandidates converts a host's JSON list of ICE candidates — the
// ice_candidates of matterjs-server's camera_provide_ice_candidates, each
// an object of ICECandidateStruct's fields (candidate, sdpMid,
// sdpMLineIndex; keys matched with case and word separators ignored) —
// into the cluster's structs for [Session.ProvideICECandidates]. It runs
// the converter [Provider.Send] runs for ProvideIceCandidates'
// iceCandidates field, so a refusal is worded as there ("ice_candidates[0]
// requires sdpMid") and matches [ErrInvalidArgument]. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraCommands.ts:342-366
// (parseIceCandidatesArgs, which converts ice_candidates through
// toProviderCommandFields) at 0bd06f2.
func ParseICECandidates(candidates json.RawMessage) ([]wpdef.ICECandidateStruct, error) {
	contract := providerContracts[cmdProvideIceCandidates]
	f := contract.byKey[canonicalKey("iceCandidates")]
	list, err := f.convert(candidates, "ice_candidates")
	if err != nil {
		return nil, err
	}
	session := contract.byKey[canonicalKey("webRtcSessionId")]
	fields := &convertedFields{contract: contract, values: map[string]wireValue{session.property: uintValue(0), f.property: list}}
	req, err := (&providerFields{name: cmdProvideIceCandidates, fields: fields}).request(0)
	if err != nil {
		return nil, err
	}
	got, ok := req.(wpdef.ProvideIceCandidatesRequest)
	if !ok {
		return nil, fmt.Errorf("camera: ProvideIceCandidates decoded as %T", req)
	}
	return got.IceCandidates, nil
}
