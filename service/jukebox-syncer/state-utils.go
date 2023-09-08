package jukebox_syncer

import (
	"fmt"
	pb "roll20-audio-bouncer/proto"
)

func findMatching(old *R20State, url string) *R20Track {
	for _, oldT := range old.Tracks {
		if oldT.Url == url {
			return &oldT
		}

	}
	return nil
}

func stateDelta(old, new *R20State) ([]*pb.Event, error) {
	if old == nil || new == nil {
		return nil, fmt.Errorf("At least one state is nil")
	}
	// If multiple records occurs at the same time, we must ensure we are comparing matching records
	if new.Rid != old.Rid {
		return nil, fmt.Errorf("mismatching state id. Old id %s, new id %s", old.Rid, new.Rid)
	}

	// With enough bad luck, some state changes could be received out of order
	// Skipping a state change isn't that bad, as we can parse multiple differences between states
	if new.Date.Before(old.Date) {
		return nil, fmt.Errorf("expected new state to be newer than old state. Got new : '%s'  | old '%s' ", new.Date, old.Date)
	}

	// Finally, we can have multiple users sending state changes for the same record
	if new.Uid != old.Uid {
		// For now, we're going to consider that a single user have to send the state.
		// A better implementation would receive the state from multiple user and dedup them
		return nil, fmt.Errorf("[Jukebox syncer] :: user ID mismatch, multiple users are updating the same record state (Got new : '%s' | old '%s')", new.Uid, old.Uid)
	}

	var events []*pb.Event
	for _, newT := range new.Tracks {
		oldT := findMatching(old, newT.Url)
		events = append(events, trackDelta(oldT, &newT, new.Rid)...)
	}
	return events, nil
}

func scanForPlay(state *R20State) ([]*pb.Event, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}
	var events []*pb.Event
	for _, track := range state.Tracks {
		if track.Playing {
			// TODO :: Should also seek to the current time
			events = append(events, makeEvent(&track, pb.EventType_PLAY, state.Rid))
		}
	}
	return events, nil
}

func trackDelta(old, new *R20Track, rId string) []*pb.Event {
	var events []*pb.Event
	// First case, the track is new and playing
	// This should not happen unless we missed an event
	if old == nil {
		if new.Playing {
			// TODO :: Handle seek
			events = append(events, makeEvent(new, pb.EventType_PLAY, rId))
		}
		// As old is nil, we can't compare anything else
		return events
	}

	// Second case, toggle the track play state
	if new.Playing != old.Playing {
		evtType := pb.EventType_PLAY
		if !new.Playing {
			evtType = pb.EventType_STOP
		}
		events = append(events, makeEvent(new, evtType, rId))
	}

	// Third case, the track is the same, but the loop state changed
	if new.Loop != old.Loop {
		events = append(events, makeEvent(new, pb.EventType_OTHER, rId))
	}

	return events
}

func makeEvent(track *R20Track, t pb.EventType, rId string) *pb.Event {
	return &pb.Event{
		RecordId: rId,
		EvtId:    track.Url,
		Type:     t,
		AssetUrl: track.Url,
		Loop:     track.Loop,
	}
}