package jukebox_syncer

import (
	"fmt"
	"log/slog"
	pb "roll20-audio-bouncer/proto"
)

type JukeboxSyncer struct {
	mixer MixerAPI
	// Mapping of the last known state of a game to its ID
	stateMap map[string]*R20State
	// Which record have already started, by ID
	startedMap map[string]bool
}

func NewJukeboxSyncer(mixer MixerAPI) *JukeboxSyncer {
	return &JukeboxSyncer{
		mixer:      mixer,
		stateMap:   map[string]*R20State{},
		startedMap: map[string]bool{},
	}
}

func (es *JukeboxSyncer) Start(id string) error {
	// Send start signal to live audio mixer
	err := es.mixer.Start(id)
	if err != nil {
		return err
	}
	es.startedMap[id] = true
	return nil
}

func (es *JukeboxSyncer) Handle(new *R20State) error {
	if new == nil {
		return fmt.Errorf("New state is nil")
	}
	if _, ok := es.startedMap[new.Rid]; !ok {
		return fmt.Errorf("Attempted to send an event for a record that hasn't started yet")
	}
	oldState, ok := es.stateMap[new.Rid]
	var events []*pb.Event
	var err error
	if !ok {
		// This is the first ever state we're receiving
		events, err = scanForPlay(new)
	} else {
		events, err = stateDelta(oldState, new)
	}
	if err != nil {
		return err
	}

	for _, evt := range events {
		err := es.mixer.Send(evt)
		// Any error here is non-fatal
		if err != nil {
			slog.Warn(fmt.Sprintf("event with url %s error %s", evt.AssetUrl, err))
		}

	}
	es.stateMap[new.Rid] = new
	return nil
}

func (es *JukeboxSyncer) Stop(id string) error {
	// Send stop signal to live audio mixer, get the storage key and get it back to the message bus
	err := es.mixer.Stop(id)
	if err != nil {
		return err
	}
	delete(es.startedMap, id)
	return nil
}
