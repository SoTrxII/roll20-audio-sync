package jukebox_syncer

import (
	"github.com/stretchr/testify/assert"
	pb "roll20-audio-bouncer/proto"
	"testing"
	"time"
)

func TestTrackDelta_OldIsNil(t *testing.T) {
	evts := trackDelta(nil, &R20Track{Playing: true}, "0")
	assert.Len(t, evts, 1, "expected 1 event")
	assert.True(t, evts[0].Type == pb.EventType_PLAY, "expected play event")
	evts = trackDelta(nil, &R20Track{Playing: false}, "0")
	assert.Len(t, evts, 0, "expected 0 event")
}

func TestTrackDelta_PlayState(t *testing.T) {
	evts := trackDelta(&R20Track{Playing: true}, &R20Track{Playing: true}, "0")
	assert.Len(t, evts, 0, "expected 0 event")
	evts = trackDelta(&R20Track{Playing: true}, &R20Track{Playing: false}, "0")
	assert.Len(t, evts, 1, "expected 1 event")
	assert.True(t, evts[0].Type == pb.EventType_STOP, "expected stop event")
	evts = trackDelta(&R20Track{Playing: false}, &R20Track{Playing: true}, "0")
	assert.Len(t, evts, 1, "expected 1 event")
	assert.True(t, evts[0].Type == pb.EventType_PLAY, "expected play event")
	evts = trackDelta(&R20Track{Playing: false}, &R20Track{Playing: false}, "0")
	assert.Len(t, evts, 0, "expected 0 event")
}

func TestTrackDelta_LoopState(t *testing.T) {
	evts := trackDelta(&R20Track{Loop: true}, &R20Track{Loop: true}, "0")
	assert.Len(t, evts, 0, "expected 0 event")
	evts = trackDelta(&R20Track{Loop: true}, &R20Track{Loop: false}, "0")
	assert.Len(t, evts, 1, "expected 1 event")
	assert.True(t, evts[0].Type == pb.EventType_OTHER, "expected other event")
	evts = trackDelta(&R20Track{Loop: false}, &R20Track{Loop: true}, "0")
	assert.Len(t, evts, 1, "expected 1 event")
	assert.True(t, evts[0].Type == pb.EventType_OTHER, "expected other event")
	evts = trackDelta(&R20Track{Loop: false}, &R20Track{Loop: false}, "0")
	assert.Len(t, evts, 0, "expected 0 event")
}

func TestStateDelta_EitherStateNil(t *testing.T) {
	evts, err := stateDelta(nil, &R20State{})
	assert.Error(t, err)
	assert.Nil(t, evts)
	evts, err = stateDelta(&R20State{}, nil)
	assert.Error(t, err)
	assert.Nil(t, evts)
}

func TestStateDelta_NoChange(t *testing.T) {
	evts, err := stateDelta(&R20State{}, &R20State{})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
}

func TestStateDelta_PlayState(t *testing.T) {
	evts, err := stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Playing: true}}}, &R20State{Tracks: []R20Track{{Url: "a", Playing: true}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Playing: true}}}, &R20State{Tracks: []R20Track{{Url: "a", Playing: false}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 1)
	assert.True(t, evts[0].Type == pb.EventType_STOP, "expected stop event")
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Playing: false}}}, &R20State{Tracks: []R20Track{{Url: "a", Playing: true}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 1)
	assert.True(t, evts[0].Type == pb.EventType_PLAY, "expected play event")
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Playing: false}}}, &R20State{Tracks: []R20Track{{Url: "a", Playing: false}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
}

func TestStateDelta_LoopState(t *testing.T) {
	evts, err := stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Loop: true}}}, &R20State{Tracks: []R20Track{{Url: "a", Loop: true}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Loop: true}}}, &R20State{Tracks: []R20Track{{Url: "a", Loop: false}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 1)
	assert.True(t, evts[0].Type == pb.EventType_OTHER, "expected other event")
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Loop: false}}}, &R20State{Tracks: []R20Track{{Url: "a", Loop: true}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 1)
	assert.True(t, evts[0].Type == pb.EventType_OTHER, "expected other event")
	evts, err = stateDelta(&R20State{Tracks: []R20Track{{Url: "a", Loop: false}}}, &R20State{Tracks: []R20Track{{Url: "a", Loop: false}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
}

func TestStateDelta_NewStateIsOlder(t *testing.T) {
	refDate := time.Now()
	oldS := &R20State{
		Date: refDate.Add(1 * time.Second),
	}
	newS := &R20State{
		Date: refDate,
	}
	evts, err := stateDelta(oldS, newS)
	assert.Error(t, err)
	assert.Nil(t, evts)
}

func TestStateDelta_MismatchingUid(t *testing.T) {
	oldS := &R20State{
		Uid: "a",
	}
	newS := &R20State{
		Uid: "b",
	}
	evts, err := stateDelta(oldS, newS)
	assert.Error(t, err)
	assert.Nil(t, evts)
}

func TestStateDelta_MismatchingRid(t *testing.T) {
	oldS := &R20State{
		Rid: "a",
	}
	newS := &R20State{
		Rid: "b",
	}
	evts, err := stateDelta(oldS, newS)
	assert.Error(t, err)
	assert.Nil(t, evts)
}

func TestScanForPlay_StateNil(t *testing.T) {
	evts, err := scanForPlay(nil)
	assert.Error(t, err)
	assert.Nil(t, evts)
}

func TestScanForPlay_NoTrack(t *testing.T) {
	evts, err := scanForPlay(&R20State{})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
}

func TestScanForPlay_TrackNotPlaying(t *testing.T) {
	evts, err := scanForPlay(&R20State{Tracks: []R20Track{{Url: "a", Playing: false}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 0)
}

func TestScanForPlay_TrackPlaying(t *testing.T) {
	evts, err := scanForPlay(&R20State{Tracks: []R20Track{{Url: "a", Playing: true}}})
	assert.NoError(t, err)
	assert.Len(t, evts, 1)
	assert.True(t, evts[0].Type == pb.EventType_PLAY, "expected play event")
}