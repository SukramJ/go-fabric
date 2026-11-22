// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package media

import (
	"context"
	"errors"
	"testing"

	channeldef "thingfabric.dev/go-fabric/cluster/spec/channel"
	"thingfabric.dev/go-fabric/cluster/spec/spectest"
)

// errTunerRefused is the refusal of cloneChannels.
var errTunerRefused = errors.New("tuner refused")

// cloneChannels is a Channel port that edits the state it is handed in
// place, through its pointers and lists, and then refuses the command.
type cloneChannels struct{}

func (cloneChannels) ChangeChannel(context.Context, *ChannelState, channeldef.ChangeChannelRequest) (channeldef.ChangeChannelResponse, error) {
	return channeldef.ChangeChannelResponse{}, nil
}

func (cloneChannels) ChangeChannelByNumber(_ context.Context, st *ChannelState, req channeldef.ChangeChannelByNumberRequest) error {
	st.CurrentChannel.MajorNumber = req.MajorNumber
	st.ChannelList[0].MajorNumber = req.MajorNumber
	*st.ChannelList[0].Name = "refused"
	return errTunerRefused
}

func (cloneChannels) SkipChannel(context.Context, *ChannelState, channeldef.SkipChannelRequest) error {
	return nil
}

// TestRefusedCommandLeavesTheCommittedState holds R2-P6-err-1460: a port
// that edits the state it is handed in place and then fails changes no
// committed value — neither what State reports nor what the store holds —
// and the next successful command does not commit it; a host's edit of a
// State copy does not reach the server either.
func TestRefusedCommandLeavesTheCommittedState(t *testing.T) {
	name := "one"
	s, err := NewChannels(ChannelsConfig{
		Port:       cloneChannels{},
		Features:   channeldef.FeatureChannelList,
		Attributes: []uint32{channeldef.AttrCurrentChannel},
		Initial: ChannelState{
			ChannelList:    []channeldef.ChannelInfoStruct{{MajorNumber: 1, Name: &name}},
			CurrentChannel: &channeldef.ChannelInfoStruct{MajorNumber: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	v0 := s.MatterDataVersion()
	if _, err := s.MatterInvoke(context.Background(), channeldef.CmdChangeChannelByNumber, channeldef.ChangeChannelByNumberRequest{MajorNumber: 7}); !errors.Is(err, errTunerRefused) {
		t.Fatalf("err = %v, want the tuner's refusal", err)
	}
	check := func(when string) {
		t.Helper()
		st := s.State()
		if st.CurrentChannel.MajorNumber != 1 || st.ChannelList[0].MajorNumber != 1 || *st.ChannelList[0].Name != "one" {
			t.Errorf("%s: State() = %+v / %+v, want the committed channel 1 \"one\"", when, *st.CurrentChannel, st.ChannelList[0])
		}
		v, _ := s.Value(channeldef.AttrCurrentChannel)
		if c, _ := v.(channeldef.ChannelInfoStruct); c.MajorNumber != 1 {
			t.Errorf("%s: the store holds CurrentChannel %+v, want 1", when, v)
		}
	}
	check("after the refused command")
	if v := s.MatterDataVersion(); v != v0 {
		t.Errorf("the refused command moved the data version %d → %d", v0, v)
	}
	if _, err := s.MatterInvoke(context.Background(), channeldef.CmdSkipChannel, channeldef.SkipChannelRequest{Count: 1}); err != nil {
		t.Fatal(err)
	}
	check("after a successful SkipChannel")
	got := s.State()
	got.CurrentChannel.MinorNumber = 42
	*got.ChannelList[0].Name = "host"
	check("after a host edits a State copy")
}

// The media states as types with a Clone, so spectest.CheckClone holds
// each server's clone to a deep copy: the class guard of R2-P6-err-1460
// (decision 142). A new stateful media server adds its state here.
type (
	appLauncherClone     AppLauncherState
	outputClone          OutputState
	appBasicClone        AppBasicState
	contentControlClone  ContentControlState
	channelClone         ChannelState
	contentLauncherClone ContentLauncherState
	inputClone           InputState
	playbackClone        PlaybackState
	targetClone          TargetState
	wakeOnLanClone       WakeOnLanState
)

func (s appLauncherClone) Clone() appLauncherClone {
	return appLauncherClone(AppLauncherState(s).Clone())
}
func (s outputClone) Clone() outputClone     { return outputClone(OutputState(s).Clone()) }
func (s appBasicClone) Clone() appBasicClone { return appBasicClone(AppBasicState(s).clone()) }
func (s contentControlClone) Clone() contentControlClone {
	return contentControlClone(ContentControlState(s).Clone())
}
func (s channelClone) Clone() channelClone { return channelClone(ChannelState(s).Clone()) }
func (s contentLauncherClone) Clone() contentLauncherClone {
	return contentLauncherClone(ContentLauncherState(s).Clone())
}
func (s inputClone) Clone() inputClone       { return inputClone(InputState(s).Clone()) }
func (s playbackClone) Clone() playbackClone { return playbackClone(PlaybackState(s).Clone()) }
func (s targetClone) Clone() targetClone     { return targetClone(TargetState(s).Clone()) }
func (s wakeOnLanClone) Clone() wakeOnLanClone {
	return wakeOnLanClone(WakeOnLanState(s).Clone())
}

func TestMediaStateClonesAreDeep(t *testing.T) {
	spectest.CheckClone[appLauncherClone](t)
	spectest.CheckClone[outputClone](t)
	spectest.CheckClone[appBasicClone](t)
	spectest.CheckClone[contentControlClone](t)
	spectest.CheckClone[channelClone](t)
	spectest.CheckClone[contentLauncherClone](t)
	spectest.CheckClone[inputClone](t)
	spectest.CheckClone[playbackClone](t)
	spectest.CheckClone[targetClone](t)
	spectest.CheckClone[wakeOnLanClone](t)
}
