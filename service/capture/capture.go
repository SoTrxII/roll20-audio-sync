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
	"time"
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
	// Another tab holds this game's capture, see captureLease
	ErrCapturedElsewhere = errors.New("this game's jukebox is already captured from another tab")
)

// One tab captures a game at a time: two GM tabs of the same game (a co-GM,
// or Roll20 open twice) would both be mixed in, doubling the music. The tab
// whose chunk arrives first owns the capture, and every chunk renews that for
// captureLease. 30s is Lullaby's longest pause between two upload retries, so
// an owner on a bad connection keeps it, while a crashed or closed tab frees
// the game for another one half a minute later
const captureLease = 30 * time.Second

// What one object can weigh through the Dapr binding: the sidecars of both this
// service and encode-box cap a request at 2500 MB, and the binding sends the
// file in base64, a third bigger. A session with the tab's picture over it
// falls back to the jukebox alone rather than failing its upload. Lullaby caps
// the picture so 4h stay near 1.3 GB
const maxUploadBytes = 1800 << 20

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
	// The clock the lease runs on, replaced in tests
	now func() time.Time
}

type record struct {
	dir string
	// Unix ms of the Discord recording's t=0, as seen by record-orchestrator.
	// 0 when unknown: the first segment then becomes t=0
	alignTo  int64
	segments map[int]*segment
	// The tab holding the capture and when it last sent a chunk. The id is
	// Lullaby's, per tab and kept across a reload of that tab
	owner     string
	lastChunk time.Time
}

// Whether a tab other than client holds the capture right now
func (rec *record) capturedElsewhere(client string, now time.Time) bool {
	return !rec.lastChunk.IsZero() && rec.owner != client && now.Sub(rec.lastChunk) < captureLease
}

// A segment is one uninterrupted MediaRecorder run. The GM stopping and
// restarting the capture (or reloading Roll20) starts a new one
type segment struct {
	// Unix ms, server clock, of the segment's first sample
	startedAt int64
	// The tab's picture is recorded with its sound
	video   bool
	nextSeq int
	file    *os.File
}

func NewRecorder(baseDir string, store Uploader, mix Mixer) *Recorder {
	return &Recorder{baseDir: baseDir, store: store, mix: mix, records: map[string]*record{}, now: time.Now}
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

// Status tells a tab whether the game is being recorded, and whether another
// tab already captures its jukebox: Lullaby then leaves it alone
func (r *Recorder) Status(id, client string) (recording, capturedElsewhere bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return false, false
	}
	return true, rec.capturedElsewhere(client, r.now())
}

// Chunk appends one MediaRecorder chunk. Lullaby sends a segment's chunks one
// at a time and retries a chunk until it's acknowledged, so a seq already
// written is a retry whose answer got lost: acknowledge it again, don't append.
//
// client is the sending tab. A payload cached from before the lease sends none,
// and the empty id is a tab like any other. video tells that the segment
// holds the tab's picture too, which its first chunk decides
func (r *Recorder) Chunk(id, client string, seg, seq int, startedAt int64, video bool, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return ErrNotRecording
	}
	now := r.now()
	if rec.capturedElsewhere(client, now) {
		return ErrCapturedElsewhere
	}
	// Any chunk shows the tab is alive, even one refused below
	rec.owner, rec.lastChunk = client, now
	s, ok := rec.segments[seg]
	if !ok {
		if seq != 0 {
			return ErrOutOfOrder
		}
		f, err := os.Create(filepath.Join(rec.dir, fmt.Sprintf("seg-%d.webm", seg)))
		if err != nil {
			return err
		}
		s = &segment{startedAt: startedAt, video: video, file: f}
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
// When segments hold the tab's picture, that mix goes with their pictures as
// "video/<id>.mkv" instead: the bucket expires that prefix after a month, the
// raw pictures being heavy and only needed until the video is on YouTube.
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
		// A segment whose chunks were all empty has nothing to mix, and ffmpeg
		// refuses an empty input ("End of file"), failing the whole mix and
		// with it every other segment's music. Seen on the VPS with a client
		// that posted empty bodies; Lullaby skips empty chunks, others may not
		if info, err := os.Stat(s.file.Name()); err != nil {
			return "", err
		} else if info.Size() == 0 {
			slog.Warn(fmt.Sprintf("[Capture] :: %s : segment %d is empty, left out", id, n))
			continue
		}
		segs = append(segs, placedSegment{path: s.file.Name(), n: n, startedAt: s.startedAt, video: s.video})
	}
	if len(segs) == 0 {
		return "", ErrNoAudio
	}

	out := filepath.Join(rec.dir, "mix.ogg")
	if err := r.mix(mixArgs(segs, rec.alignTo, out)); err != nil {
		return "", err
	}
	key := id + ".ogg"
	if picture := pictureArgs(segs, rec.alignTo, out, filepath.Join(rec.dir, "capture.mkv")); picture != nil {
		// Losing the picture must not lose the music: on any problem, the mix alone
		withPicture := picture[len(picture)-1]
		if err := r.mix(picture); err != nil {
			slog.Warn(fmt.Sprintf("[Capture] :: %s : keeping the jukebox alone, adding the picture failed : %v", id, err))
		} else if info, err := os.Stat(withPicture); err != nil {
			slog.Warn(fmt.Sprintf("[Capture] :: %s : keeping the jukebox alone : %v", id, err))
		} else if info.Size() > maxUploadBytes {
			slog.Warn(fmt.Sprintf("[Capture] :: %s : keeping the jukebox alone, %d MB with the picture is too big to upload", id, info.Size()>>20))
		} else {
			out, key = withPicture, "video/"+id+".mkv"
		}
	}
	if err := r.store.Upload(out, key); err != nil {
		return "", err
	}
	return key, nil
}

type placedSegment struct {
	path      string
	n         int
	startedAt int64
	video     bool
}

// The Discord t=0, or the first segment's start when it isn't known
func timelineStart(segs []placedSegment, alignTo int64) int64 {
	if alignTo != 0 {
		return alignTo
	}
	alignTo = segs[0].startedAt
	for _, s := range segs {
		alignTo = min(alignTo, s.startedAt)
	}
	return alignTo
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
	alignTo = timelineStart(segs, alignTo)

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

// pictureArgs builds the ffmpeg command putting the jukebox mix and the picture
// of each segment that has one in one file, nil when none has. Copied, not
// encoded: 0.5s for 20 min, against ~25 min of encoding for a 4h session, which
// /session end could not wait for. Each picture becomes its own track, starting
// where the segment started on the Discord timeline; encode-box lays them over
// a black background. ffmpeg would refuse to join captures whose size or codec
// differ into one track, and the gaps between them stay empty this way.
//
// A segment starting before the Discord t=0 starts at 0: it is the same few
// tens of ms of uncertainty as in mixArgs, invisible on a picture
func pictureArgs(segs []placedSegment, alignTo int64, mix, out string) []string {
	alignTo = timelineStart(segs, alignTo)
	args := []string{"-hide_banner", "-nostdin", "-y", "-i", mix}
	maps := []string{"-map", "0:a"}
	inputs := 1
	for _, s := range segs {
		if !s.video {
			continue
		}
		args = append(args, "-itsoffset", fmt.Sprintf("%.3f", float64(max(0, s.startedAt-alignTo))/1000), "-i", s.path)
		// "?": a segment announced with a picture but without one is skipped
		maps = append(maps, "-map", fmt.Sprintf("%d:v:0?", inputs))
		inputs++
	}
	if inputs == 1 {
		return nil
	}
	return append(append(args, maps...), "-c", "copy", out)
}
