package jukebox_syncer

import (
	"github.com/stretchr/testify/assert"
	pb "roll20-audio-bouncer/proto"
	"testing"
)

func TestJukeboxSyncer_HandleStateIsNil(t *testing.T) {
	s := NewJukeboxSyncer(&mockMixer{})
	err := s.Handle(nil)
	assert.Error(t, err)
}

// Assert they're no state retention when a recording stops
func TestJukeboxSyncer_StateDeletion(t *testing.T) {
	s := NewJukeboxSyncer(&mockMixer{})
	err := s.Start("1")
	assert.NoError(t, err)
	err = s.Handle(&R20State{
		Rid: "1",
		Uid: "2",
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	})
	assert.NoError(t, err)
	err = s.Stop("1")
	assert.NoError(t, err)
	err = s.Start("1")
	assert.NoError(t, err)
	// If state has been kept, this will throw an error
	// as the user ID is different
	err = s.Handle(&R20State{
		Rid: "1",
		Uid: "3",
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	})
	assert.NoError(t, err)

}
func TestJukeboxSyncer_HandleStateBeforeStartError(t *testing.T) {
	s := NewJukeboxSyncer(&mockMixer{})
	err := s.Handle(&R20State{
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	})
	assert.Error(t, err)
}
func TestJukeboxSyncer_HandleFirstState(t *testing.T) {
	s := NewJukeboxSyncer(&mockMixer{})
	err := s.Start("1")
	assert.NoError(t, err)
	err = s.Handle(&R20State{
		Rid: "1",
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	})
	assert.NoError(t, err)
}
func TestJukeboxSyncer_HandleStateDelta(t *testing.T) {
	s := NewJukeboxSyncer(&mockMixer{})
	err := s.Start("1")
	assert.NoError(t, err)
	err = s.Handle(&R20State{
		Rid: "1",
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	})
	assert.NoError(t, err)
	err = s.Handle(&R20State{
		Rid: "1",
		Tracks: []R20Track{
			{
				Url:     "a",
				Playing: false,
			},
		},
	})
	assert.NoError(t, err)
}

type mockMixer struct {
	MixerAPI
}

func (m *mockMixer) Send(evt *pb.Event) error {
	return nil
}
func (m *mockMixer) Start(id string) error {
	return nil
}

func (m *mockMixer) Stop(id string) error {
	return nil
}
