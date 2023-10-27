package jukebox_syncer

import (
	"fmt"
	"log/slog"
	"math"
	pb "roll20-audio-bouncer/proto"
	"strconv"
	"strings"
	"time"
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
			// TODO :: Handle initial seek
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

	// Fourth case, the track is the same, but the volume changed
	if new.Volume != old.Volume {
		evt := makeEvent(new, pb.EventType_VOLUME, rId)
		evt.VolumeDeltaDb = computeVolumeDb(old.Volume/100, new.Volume/100)
		events = append(events, evt)
	}

	// Fifth case, the track is the same, but the seek position changed
	// Roll20 is doing this in a weird way
	// When the user seek into a track, the progress changes, but not the actual position, which is updated later
	// To properly get the new position, we must multiply the progress percentage by the duration
	if new.Playing && old.Playing && new.Progress != old.Progress {
		if d, err := parseDuration(new.Duration); err == nil {
			evt := makeEvent(new, pb.EventType_SEEK, rId)
			evt.SeekPositionSec = int64(d.Seconds() * math.Min(new.Progress, 1))
			events = append(events, evt)
		} else {
			slog.Warn(fmt.Sprintf("[Jukebox syncer] :: Ignoring SEEK event, error while parsing seek pos %s : %v", new.Duration, err))
		}
	}

	return events
}

// Provided two volume values from 0.001 to 1, compute the difference in decibels
// Any value out of the range will be clamped to the closest bound
func computeVolumeDb(old, new float64) float64 {
	// 20 * log_10(1.0E-3/0.01) = -60, which is the minimum volume
	// As we approach 0, log approaches -inf, so we clamp the value to -60
	// When user is setting the volume to 0, he wants to mute the track
	const lower = 1.0e-3
	// 20 * log_10(1/0.001) = 60, which is the maximum volume
	const upper = 1
	cOld := math.Max(lower, math.Min(upper, old))
	cNew := math.Max(lower, math.Min(upper, new))

	return 20 * math.Log10(cNew/cOld)
}

// Parse either a number of seconds, a mm:ss or hh:mm:ss string into a duration
func parseDuration(s string) (time.Duration, error) {
	d, err := strconv.Atoi(s)
	if err == nil {
		return time.Second * time.Duration(d), nil
	}
	parts := strings.Split(s, ":")
	if len(parts) == 2 {
		min, _ := strconv.Atoi(parts[0])
		sec, _ := strconv.Atoi(parts[1])
		return time.Duration(min)*time.Minute + time.Duration(sec)*time.Second, nil
	} else if len(parts) == 3 {
		hr, _ := strconv.Atoi(parts[0])
		min, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		return time.Duration(hr)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec)*time.Second, nil
	}
	return 0, fmt.Errorf("invalid format")
}

func makeEvent(track *R20Track, t pb.EventType, rId string) *pb.Event {
	return &pb.Event{
		RecordId: rId,
		EvtId:    track.Url,
		Type:     t,
		AssetUrl: track.Url,
		Loop:     track.Loop,
		// Roll20 doesn't play track at full volume by default
		VolumeDeltaDb: computeVolumeDb(1, track.Volume/100),
	}
}
