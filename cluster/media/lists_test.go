// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package media_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/media"
	"github.com/SukramJ/go-fabric/cluster/spec"
	outputdef "github.com/SukramJ/go-fabric/cluster/spec/audiooutput"
	channeldef "github.com/SukramJ/go-fabric/cluster/spec/channel"
	inputdef "github.com/SukramJ/go-fabric/cluster/spec/mediainput"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	targetdef "github.com/SukramJ/go-fabric/cluster/spec/targetnavigator"
	"github.com/SukramJ/go-fabric/im"
)

// --- AudioOutput -------------------------------------------------------

type outputs struct {
	renames []outputdef.RenameOutputRequest
}

func (o *outputs) SelectOutput(_ context.Context, st *media.OutputState, req outputdef.SelectOutputRequest) error {
	for _, out := range st.OutputList {
		if out.Index == req.Index {
			st.CurrentOutput = req.Index
			return nil
		}
	}
	return errPort
}

func (o *outputs) RenameOutput(_ context.Context, st *media.OutputState, req outputdef.RenameOutputRequest) error {
	o.renames = append(o.renames, req)
	for i := range st.OutputList {
		if st.OutputList[i].Index == req.Index {
			st.OutputList[i].Name = req.Name
		}
	}
	return nil
}

type selectOnlyOutputs struct{ inner outputs }

func (s *selectOnlyOutputs) SelectOutput(ctx context.Context, st *media.OutputState, req outputdef.SelectOutputRequest) error {
	return s.inner.SelectOutput(ctx, st, req)
}

func hdmiOutputs() []outputdef.OutputInfoStruct {
	return []outputdef.OutputInfoStruct{
		{Index: 1, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
		{Index: 2, OutputType: outputdef.OutputTypeHdmi, Name: "HDMI"},
	}
}

// TestAudioOutput: SelectOutput and RenameOutput reach the port, whose
// change to the list and the current output the attributes then read; a
// renamed entry does not alias the state the host handed in.
func TestAudioOutput(t *testing.T) {
	t.Parallel()
	port := &outputs{}
	initial := hdmiOutputs()
	srv, err := media.NewOutputs(media.OutputsConfig{
		Port: port, Features: outputdef.FeatureNameUpdates,
		Initial: media.OutputState{OutputList: initial, CurrentOutput: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, outputdef.Definition, uint32(outputdef.FeatureNameUpdates))
	if _, err := invoke(t, srv, outputdef.Definition, outputdef.CmdSelectOutput, outputdef.SelectOutputRequest{Index: 2}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, outputdef.AttrCurrentOutput); v != uint8(2) {
		t.Errorf("CurrentOutput %v", v)
	}
	_, err = invoke(t, srv, outputdef.Definition, outputdef.CmdSelectOutput, outputdef.SelectOutputRequest{Index: 9})
	wantStatus(t, err, im.StatusFailure)
	if _, err := invoke(t, srv, outputdef.Definition, outputdef.CmdRenameOutput, outputdef.RenameOutputRequest{Index: 1, Name: "CertTest"}); err != nil {
		t.Fatal(err)
	}
	list := read(t, srv, outputdef.AttrOutputList).(spec.List[outputdef.OutputInfoStruct])
	if list[0].Name != "CertTest" || initial[0].Name != "HDMI" {
		t.Errorf("OutputList %+v, initial %+v", list, initial)
	}
	spectest.RoundTripAttribute(t, outputdef.Definition, outputdef.AttrOutputList, list)
	if len(port.renames) != 1 || port.renames[0].Name != "CertTest" {
		t.Errorf("renames %+v", port.renames)
	}
	if st := srv.State(); st.CurrentOutput != 2 || st.OutputList[0].Name != "CertTest" {
		t.Errorf("State %+v", st)
	}
}

// TestAudioOutputConstruction: no OutputList is ErrMissingState; NameUpdates
// without RenameOutput is ErrNotImplemented; without NU RenameOutput is not
// accepted.
func TestAudioOutputConstruction(t *testing.T) {
	t.Parallel()
	if _, err := media.NewOutputs(media.OutputsConfig{Port: &outputs{}}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("no OutputList: %v", err)
	}
	if _, err := media.NewOutputs(media.OutputsConfig{
		Port: &selectOnlyOutputs{}, Features: outputdef.FeatureNameUpdates,
		Initial: media.OutputState{OutputList: hdmiOutputs()},
	}); !errors.Is(err, media.ErrNotImplemented) {
		t.Errorf("NU without RenameOutput: %v", err)
	}
	srv, err := media.NewOutputs(media.OutputsConfig{Port: &outputs{}, Initial: media.OutputState{OutputList: hdmiOutputs()}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = invoke(t, srv, outputdef.Definition, outputdef.CmdRenameOutput, outputdef.RenameOutputRequest{Index: 1, Name: "x"})
	wantStatus(t, err, im.StatusUnsupportedCommand)
	if _, err := media.NewOutputs(media.OutputsConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
	// A state the port leaves without its list fails the command and
	// commits nothing.
	if err := srv.SetState(media.OutputState{}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("SetState without OutputList: %v", err)
	}
}

// --- MediaInput --------------------------------------------------------

type inputs struct{ shown, hidden int }

func (p *inputs) SelectInput(_ context.Context, st *media.InputState, req inputdef.SelectInputRequest) error {
	st.CurrentInput = req.Index
	return nil
}

func (p *inputs) ShowInputStatus(context.Context, *media.InputState) error {
	p.shown++
	return nil
}

func (p *inputs) HideInputStatus(context.Context, *media.InputState) error {
	p.hidden++
	return nil
}

func (p *inputs) RenameInput(_ context.Context, st *media.InputState, req inputdef.RenameInputRequest) error {
	for i := range st.InputList {
		if st.InputList[i].Index == req.Index {
			st.InputList[i].Name = req.Name
			return nil
		}
	}
	return media.StatusError{Status: im.StatusInvalidCommand}
}

type fixedInputs struct{ inner inputs }

func (p *fixedInputs) SelectInput(ctx context.Context, st *media.InputState, req inputdef.SelectInputRequest) error {
	return p.inner.SelectInput(ctx, st, req)
}

func (p *fixedInputs) ShowInputStatus(ctx context.Context, st *media.InputState) error {
	return p.inner.ShowInputStatus(ctx, st)
}

func (p *fixedInputs) HideInputStatus(ctx context.Context, st *media.InputState) error {
	return p.inner.HideInputStatus(ctx, st)
}

// TestMediaInput: the four commands reach the port; the state it changes
// is what InputList and CurrentInput read.
func TestMediaInput(t *testing.T) {
	t.Parallel()
	port := &inputs{}
	list := []inputdef.InputInfoStruct{
		{Index: 1, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "HDMI 1"},
		{Index: 2, InputType: inputdef.InputTypeHdmi, Name: "HDMI", Description: "HDMI 2"},
	}
	srv, err := media.NewInputs(media.InputsConfig{
		Port: port, Features: inputdef.FeatureNameUpdates,
		Initial: media.InputState{InputList: list, CurrentInput: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, inputdef.Definition, uint32(inputdef.FeatureNameUpdates))
	if _, err := invoke(t, srv, inputdef.Definition, inputdef.CmdSelectInput, inputdef.SelectInputRequest{Index: 2}); err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, inputdef.AttrCurrentInput); v != uint8(2) {
		t.Errorf("CurrentInput %v", v)
	}
	for _, cmd := range []uint32{inputdef.CmdShowInputStatus, inputdef.CmdHideInputStatus} {
		var req spec.Encodable = inputdef.ShowInputStatusRequest{}
		if cmd == inputdef.CmdHideInputStatus {
			req = inputdef.HideInputStatusRequest{}
		}
		if _, err := invoke(t, srv, inputdef.Definition, cmd, req); err != nil {
			t.Fatal(err)
		}
	}
	if port.shown != 1 || port.hidden != 1 {
		t.Errorf("shown %d, hidden %d", port.shown, port.hidden)
	}
	if _, err := invoke(t, srv, inputdef.Definition, inputdef.CmdRenameInput, inputdef.RenameInputRequest{Index: 1, Name: "A1"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv, inputdef.AttrInputList).(spec.List[inputdef.InputInfoStruct]); got[0].Name != "A1" || list[0].Name != "HDMI" {
		t.Errorf("InputList %+v", got)
	}
	_, err = invoke(t, srv, inputdef.Definition, inputdef.CmdRenameInput, inputdef.RenameInputRequest{Index: 7, Name: "A1"})
	wantStatus(t, err, im.StatusInvalidCommand)

	if _, err := media.NewInputs(media.InputsConfig{
		Port: &fixedInputs{}, Features: inputdef.FeatureNameUpdates,
		Initial: media.InputState{InputList: list},
	}); !errors.Is(err, media.ErrNotImplemented) {
		t.Errorf("NU without RenameInput: %v", err)
	}
	if _, err := media.NewInputs(media.InputsConfig{Port: &fixedInputs{}}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("no InputList: %v", err)
	}
	if _, err := media.NewInputs(media.InputsConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// --- TargetNavigator ---------------------------------------------------

type targets struct{}

func (targets) NavigateTarget(_ context.Context, st *media.TargetState, req targetdef.NavigateTargetRequest) (targetdef.NavigateTargetResponse, error) {
	for _, tg := range st.TargetList {
		if tg.Identifier == req.Target {
			st.CurrentTarget = req.Target
			return targetdef.NavigateTargetResponse{Status: targetdef.StatusSuccess, Data: spectest.Ptr("data response")}, nil
		}
	}
	return targetdef.NavigateTargetResponse{Status: targetdef.StatusTargetNotFound, Data: spectest.Ptr("error")}, nil
}

// TestTargetNavigator: NavigateTarget reaches the port and its response
// goes out; CurrentTarget follows the port.
func TestTargetNavigator(t *testing.T) {
	t.Parallel()
	srv, err := media.NewTargets(media.TargetsConfig{
		Port:       targets{},
		Attributes: []uint32{targetdef.AttrCurrentTarget},
		Events:     []uint32{targetdef.EventTargetUpdated},
		Initial: media.TargetState{
			TargetList:    []targetdef.TargetInfoStruct{{Identifier: 1, Name: "home"}, {Identifier: 2, Name: "settings"}},
			CurrentTarget: 0xFF,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, targetdef.Definition, 0)
	if v := read(t, srv, targetdef.AttrCurrentTarget); v != uint8(0xFF) {
		t.Errorf("CurrentTarget %v", v)
	}
	resp, err := invoke(t, srv, targetdef.Definition, targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 2})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, targetdef.Definition, targetdef.CmdNavigateTargetResponse, resp.(targetdef.NavigateTargetResponse),
		targetdef.NavigateTargetResponse{Status: targetdef.StatusSuccess, Data: spectest.Ptr("data response")})
	if v := read(t, srv, targetdef.AttrCurrentTarget); v != uint8(2) {
		t.Errorf("CurrentTarget %v", v)
	}
	resp, _ = invoke(t, srv, targetdef.Definition, targetdef.CmdNavigateTarget, targetdef.NavigateTargetRequest{Target: 9})
	if resp.(targetdef.NavigateTargetResponse).Status != targetdef.StatusTargetNotFound {
		t.Errorf("unknown target: %+v", resp)
	}
	ev := &emitted{}
	srv.SetMatterEventEmitter(ev)
	if err := srv.EmitTargetUpdated(targetdef.TargetUpdatedEvent{CurrentTarget: spectest.Ptr(uint8(2))}); err != nil || len(ev.events) != 1 {
		t.Errorf("TargetUpdated: %v %+v", err, ev.events)
	}
	if _, err := media.NewTargets(media.TargetsConfig{Port: targets{}}); !errors.Is(err, media.ErrMissingState) {
		t.Errorf("no TargetList: %v", err)
	}
	if _, err := media.NewTargets(media.TargetsConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}

// --- Channel -----------------------------------------------------------

type tuner struct {
	recorded []string
}

func (tn *tuner) ChangeChannel(_ context.Context, st *media.ChannelState, req channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	var hits []channeldef.ChannelInfoStruct
	for _, c := range st.ChannelList {
		if c.Name != nil && *c.Name == req.Match {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 0:
		return channeldef.ChangeChannelResponse{Status: channeldef.StatusNoMatches}, nil
	case 1:
		st.CurrentChannel = &hits[0]
		return channeldef.ChangeChannelResponse{Status: channeldef.StatusSuccess}, nil
	}
	return channeldef.ChangeChannelResponse{Status: channeldef.StatusMultipleMatches}, nil
}

func (tn *tuner) ChangeChannelByNumber(_ context.Context, st *media.ChannelState, req channeldef.ChangeChannelByNumberRequest) error {
	for i, c := range st.ChannelList {
		if c.MajorNumber == req.MajorNumber && c.MinorNumber == req.MinorNumber {
			st.CurrentChannel = &st.ChannelList[i]
			return nil
		}
	}
	return errPort
}

func (tn *tuner) SkipChannel(_ context.Context, st *media.ChannelState, req channeldef.SkipChannelRequest) error {
	i := slices.IndexFunc(st.ChannelList, func(c channeldef.ChannelInfoStruct) bool {
		return st.CurrentChannel != nil && c.MajorNumber == st.CurrentChannel.MajorNumber && c.MinorNumber == st.CurrentChannel.MinorNumber
	})
	n := len(st.ChannelList)
	st.CurrentChannel = &st.ChannelList[((i+int(req.Count))%n+n)%n]
	return nil
}

func (tn *tuner) GetProgramGuide(_ context.Context, _ *media.ChannelState, _ channeldef.GetProgramGuideRequest) (channeldef.ProgramGuideResponse, error) {
	return channeldef.ProgramGuideResponse{ProgramList: []channeldef.ProgramStruct{}}, nil
}

func (tn *tuner) RecordProgram(_ context.Context, _ *media.ChannelState, req channeldef.RecordProgramRequest) error {
	tn.recorded = append(tn.recorded, req.ProgramIdentifier)
	return nil
}

func (tn *tuner) CancelRecordProgram(_ context.Context, _ *media.ChannelState, req channeldef.CancelRecordProgramRequest) error {
	tn.recorded = slices.DeleteFunc(tn.recorded, func(id string) bool { return id == req.ProgramIdentifier })
	return nil
}

type basicTuner struct{ inner tuner }

func (b *basicTuner) ChangeChannel(ctx context.Context, st *media.ChannelState, req channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	return b.inner.ChangeChannel(ctx, st, req)
}

func (b *basicTuner) ChangeChannelByNumber(ctx context.Context, st *media.ChannelState, req channeldef.ChangeChannelByNumberRequest) error {
	return b.inner.ChangeChannelByNumber(ctx, st, req)
}

func (b *basicTuner) SkipChannel(ctx context.Context, st *media.ChannelState, req channeldef.SkipChannelRequest) error {
	return b.inner.SkipChannel(ctx, st, req)
}

func channels() []channeldef.ChannelInfoStruct {
	return []channeldef.ChannelInfoStruct{
		{MajorNumber: 6, MinorNumber: 0, Name: spectest.Ptr("ABC")},
		{MajorNumber: 9, MinorNumber: 1, Name: spectest.Ptr("PBS")},
		{MajorNumber: 9, MinorNumber: 2, Name: spectest.Ptr("PBS Kids")},
	}
}

// TestChannel: the channel commands reach the port; CurrentChannel and the
// responses follow it (the CHANNEL-5.2 / 5.3 sequence).
func TestChannel(t *testing.T) {
	t.Parallel()
	port := &tuner{}
	list := channels()
	srv, err := media.NewChannels(media.ChannelsConfig{
		Port:       port,
		Features:   channeldef.FeatureChannelList | channeldef.FeatureLineupInfo | channeldef.FeatureElectronicGuide | channeldef.FeatureRecordProgram,
		Attributes: []uint32{channeldef.AttrCurrentChannel},
		Initial: media.ChannelState{
			ChannelList:    list,
			Lineup:         &channeldef.LineupInfoStruct{OperatorName: "Comcast", LineupInfoType: channeldef.LineupInfoTypeMso},
			CurrentChannel: &list[0],
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	spectest.CheckServer(t, srv, channeldef.Definition, 0xF)
	if _, err := invoke(t, srv, channeldef.Definition, channeldef.CmdChangeChannelByNumber, channeldef.ChangeChannelByNumberRequest{MajorNumber: 9, MinorNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if c := read(t, srv, channeldef.AttrCurrentChannel).(channeldef.ChannelInfoStruct); c.MajorNumber != 9 || c.MinorNumber != 1 {
		t.Errorf("CurrentChannel %+v", c)
	}
	_, err = invoke(t, srv, channeldef.Definition, channeldef.CmdChangeChannelByNumber, channeldef.ChangeChannelByNumberRequest{MajorNumber: 1, MinorNumber: 1})
	wantStatus(t, err, im.StatusFailure)
	if _, err := invoke(t, srv, channeldef.Definition, channeldef.CmdSkipChannel, channeldef.SkipChannelRequest{Count: -2}); err != nil {
		t.Fatal(err)
	}
	if c := read(t, srv, channeldef.AttrCurrentChannel).(channeldef.ChannelInfoStruct); c.MajorNumber != 9 || c.MinorNumber != 2 {
		t.Errorf("CurrentChannel after SkipChannel(-2) %+v", c)
	}
	resp, err := invoke(t, srv, channeldef.Definition, channeldef.CmdChangeChannel, channeldef.ChangeChannelRequest{Match: "ABC"})
	if err != nil {
		t.Fatal(err)
	}
	wantResponse(t, channeldef.Definition, channeldef.CmdChangeChannelResponse, resp.(channeldef.ChangeChannelResponse),
		channeldef.ChangeChannelResponse{Status: channeldef.StatusSuccess})
	resp, _ = invoke(t, srv, channeldef.Definition, channeldef.CmdChangeChannel, channeldef.ChangeChannelRequest{Match: "none"})
	if resp.(channeldef.ChangeChannelResponse).Status != channeldef.StatusNoMatches {
		t.Errorf("no match: %+v", resp)
	}
	if v := read(t, srv, channeldef.AttrLineup).(channeldef.LineupInfoStruct); v.OperatorName != "Comcast" {
		t.Errorf("Lineup %+v", v)
	}
	resp, err = invoke(t, srv, channeldef.Definition, channeldef.CmdGetProgramGuide, channeldef.GetProgramGuideRequest{StartTime: 0, EndTime: 3600})
	if err != nil {
		t.Fatal(err)
	}
	spectest.RoundTripCommand(t, channeldef.Definition, channeldef.CmdProgramGuideResponse, spec.Response, resp.(channeldef.ProgramGuideResponse))
	if _, err := invoke(t, srv, channeldef.Definition, channeldef.CmdRecordProgram, channeldef.RecordProgramRequest{ProgramIdentifier: "p1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(t, srv, channeldef.Definition, channeldef.CmdCancelRecordProgram, channeldef.CancelRecordProgramRequest{ProgramIdentifier: "p1"}); err != nil {
		t.Fatal(err)
	}
	if len(port.recorded) != 0 {
		t.Errorf("recorded %v", port.recorded)
	}
}

// TestChannelDefaults: ChannelList defaults to the empty list
// (channel.element.ts:31), Lineup and CurrentChannel to null; EG without
// the guide is ErrNotImplemented, and without EG GetProgramGuide is not
// accepted.
func TestChannelDefaults(t *testing.T) {
	t.Parallel()
	srv, err := media.NewChannels(media.ChannelsConfig{
		Port: &basicTuner{}, Features: channeldef.FeatureChannelList | channeldef.FeatureLineupInfo,
		Attributes: []uint32{channeldef.AttrCurrentChannel},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v := read(t, srv, channeldef.AttrChannelList).(spec.List[channeldef.ChannelInfoStruct]); v == nil || len(v) != 0 {
		t.Errorf("ChannelList %#v, want the empty list", v)
	}
	if v := read(t, srv, channeldef.AttrLineup); v != nil {
		t.Errorf("Lineup %v, want null", v)
	}
	if v := read(t, srv, channeldef.AttrCurrentChannel); v != nil {
		t.Errorf("CurrentChannel %v, want null", v)
	}
	_, err = invoke(t, srv, channeldef.Definition, channeldef.CmdGetProgramGuide, channeldef.GetProgramGuideRequest{})
	wantStatus(t, err, im.StatusUnsupportedCommand)
	if _, err := media.NewChannels(media.ChannelsConfig{Port: &basicTuner{}, Features: channeldef.FeatureElectronicGuide}); !errors.Is(err, media.ErrNotImplemented) {
		t.Errorf("EG without the guide: %v", err)
	}
	if _, err := media.NewChannels(media.ChannelsConfig{}); !errors.Is(err, media.ErrNilPort) {
		t.Errorf("no port: %v", err)
	}
}
