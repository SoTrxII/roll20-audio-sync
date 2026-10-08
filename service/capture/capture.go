// Package capture receives the jukebox audio recorded in the GM's browser
// (Lullaby's tab capture) and turns it into one ogg aligned with the Discord
// recording.
//
// It replaced a server-side reconstruction: the browser used to report the
// jukebox *state*, and live-audio-mixer re-downloaded and re-mixed every track.
// That broke on every Roll20 or source-URL change, and with any state shape the
// diff didn't expect. The browser already plays the exact mix, so we record that.
package capture

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNotRecording     = errors.New("no recording in progress with this id")
	ErrAlreadyRecording = errors.New("a recording with this id is already in progress")
	// The first chunk of a segment carries the webm header: a segment that
	// doesn't start at seq 0 can't be decoded, and a gap in the sequence
	// would corrupt it
	ErrOutOfOrder = errors.New("chunk out of order")
	// The GM never started the capture: there is no jukebox audio to hand over
	ErrNoAudio = errors.New("no audio was captured")
)

type Uploader interface {
	Upload(path, key string) error
}

// Mixer runs ffmpeg with the given arguments
type Mixer func(args []string) error

type Recorder struct {
	baseDir string
	store   Uploader
	mix     Mixer
	mu      sync.Mutex
	records map[string]*record
}

type record struct {
	dir string
	// Unix ms of the Discord recording's t=0, as seen by record-orchestrator.
	// 0 when unknown: the first segment then becomes t=0
	alignTo  int64
	segments map[int]*segment
}

// A segment is one uninterrupted MediaRecorder run. The GM stopping and
// restarting the capture (or reloading Roll20) starts a new one
type segment struct {
	// Unix ms, server clock, of the segment's first sample
	startedAt int64
	nextSeq   int
	file      *os.File
}

func NewRecorder(baseDir string, store Uploader, mix Mixer) *Recorder {
	return &Recorder{baseDir: baseDir, store: store, mix: mix, records: map[string]*record{}}
}

// FFmpeg is the production Mixer
func FFmpeg(args []string) error {
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		// ffmpeg's last lines hold the actual reason
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.Join(lines[max(0, len(lines)-3):], " | "))
	}
	return nil
}

func (r *Recorder) Start(id string, alignTo int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[id]; ok {
		return ErrAlreadyRecording
	}
	dir := filepath.Join(r.baseDir, id)
	// A leftover from a crashed run would be mixed into this one
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	r.records[id] = &record{dir: dir, alignTo: alignTo, segments: map[int]*segment{}}
	return nil
}

func (r *Recorder) IsRecording(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.records[id]
	return ok
}

// Chunk appends one MediaRecorder chunk. Lullaby sends a segment's chunks one
// at a time and retries a chunk until it's acknowledged, so a seq already
// written is a retry whose answer got lost: acknowledge it again, don't append
func (r *Recorder) Chunk(id string, seg, seq int, startedAt int64, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return ErrNotRecording
	}
	s, ok := rec.segments[seg]
	if !ok {
		if seq != 0 {
			return ErrOutOfOrder
		}
		f, err := os.Create(filepath.Join(rec.dir, fmt.Sprintf("seg-%d.webm", seg)))
		if err != nil {
			return err
		}
		s = &segment{startedAt: startedAt, file: f}
		rec.segments[seg] = s
	}
	switch {
	case seq < s.nextSeq:
		return nil
	case seq > s.nextSeq:
		return ErrOutOfOrder
	}
	if _, err := s.file.Write(data); err != nil {
		return err
	}
	s.nextSeq++
	return nil
}

// Stop mixes every segment at its place on the Discord timeline, uploads the
// result as "<id>.ogg" (the key record-orchestrator hands on) and cleans up.
//
// It is synchronous, so /session end waits for the re-encode: measured 10.7s
// for 30 min of audio on a desktop, so ~1.5 min for a 4h session there, more
// on the VPS. Velvet defers its reply, which gives it 15 min
func (r *Recorder) Stop(id string) (string, error) {
	r.mu.Lock()
	rec, ok := r.records[id]
	// Chunks still in flight now get ErrNotRecording, which tells Lullaby to stop
	delete(r.records, id)
	r.mu.Unlock()
	if !ok {
		return "", ErrNotRecording
	}
	defer func() {
		if err := os.RemoveAll(rec.dir); err != nil {
			slog.Warn(fmt.Sprintf("[Capture] :: could not clean %s : %v", rec.dir, err))
		}
	}()

	var segs []placedSegment
	for n, s := range rec.segments {
		if err := s.file.Close(); err != nil {
			return "", err
		}
		segs = append(segs, placedSegment{path: s.file.Name(), n: n, startedAt: s.startedAt})
	}
	if len(segs) == 0 {
		return "", ErrNoAudio
	}

	out := filepath.Join(rec.dir, "mix.ogg")
	if err := r.mix(mixArgs(segs, rec.alignTo, out)); err != nil {
		return "", err
	}
	key := id + ".ogg"
	if err := r.store.Upload(out, key); err != nil {
		return "", err
	}
	return key, nil
}

type placedSegment struct {
	path      string
	n         int
	startedAt int64
}

// mixArgs builds the ffmpeg command placing each segment at
// (startedAt - alignTo) on the Discord timeline: delayed when it started after
// Discord did (the usual case, the GM clicks Record after /session start),
// trimmed when it started before.
//
// ponytail: alignment is as good as two estimates: record-orchestrator stamps
// alignTo when Pandora's start reply arrives (pub/sub latency, tens of ms), and
// Lullaby converts its clock to the server's with one round trip (RTT/2).
// Also, encode-box concatenates Discord chunks after a reconnect without the
// gap, so the music drifts by that gap after a reconnect. Both inaudible for
// background music; fix the second in encode-box if it ever matters
func mixArgs(segs []placedSegment, alignTo int64, out string) []string {
	sort.Slice(segs, func(i, j int) bool { return segs[i].n < segs[j].n })
	if alignTo == 0 {
		alignTo = segs[0].startedAt
		for _, s := range segs {
			alignTo = min(alignTo, s.startedAt)
		}
	}

	args := []string{"-hide_banner", "-nostdin", "-y"}
	var graph []string
	var labels string
	for i, s := range segs {
		args = append(args, "-i", s.path)
		place := "anull"
		if off := s.startedAt - alignTo; off > 0 {
			place = fmt.Sprintf("adelay=%d:all=1", off)
		} else if off < 0 {
			place = fmt.Sprintf("atrim=start=%.3f,asetpts=PTS-STARTPTS", float64(-off)/1000)
		}
		graph = append(graph, fmt.Sprintf("[%d:a]%s[a%d]", i, place, i))
		labels += fmt.Sprintf("[a%d]", i)
	}
	// normalize=0: segments never overlap, amix must not halve their volume
	graph = append(graph, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=longest[out]", labels, len(segs)))

	return append(args,
		"-filter_complex", strings.Join(graph, ";"),
		"-map", "[out]",
		// Background music, mixed at 0.22 volume by encode-box: 96k is plenty
		"-c:a", "libopus", "-b:a", "96k",
		out,
	)
}
