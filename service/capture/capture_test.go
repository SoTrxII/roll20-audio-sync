package capture

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type copyUploader struct{ dst, key string }

func (u *copyUploader) Upload(path, key string) error {
	u.key = key
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(u.dst, data, 0o644)
}

func noMix([]string) error { return nil }

type keysUploader struct{ keys []string }

func (u *keysUploader) Upload(_, key string) error {
	u.keys = append(u.keys, key)
	return nil
}

func TestChunk_RefusesUnknownRecord(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	assert.ErrorIs(t, r.Chunk("1", "tab", 0, 0, 0, false, []byte("x")), ErrNotRecording)
}

func TestChunk_SequenceIsIdempotentAndOrdered(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	require.NoError(t, r.Start("1", 0))
	// Closes the segment file, which Windows won't let TempDir delete while open.
	// The upload fails (noMix wrote nothing), which is irrelevant here
	t.Cleanup(func() { _, _ = r.Stop("1") })

	assert.ErrorIs(t, r.Chunk("1", "tab", 0, 1, 0, false, []byte("b")), ErrOutOfOrder, "a segment must start with its header chunk")
	require.NoError(t, r.Chunk("1", "tab", 0, 0, 0, false, []byte("a")))
	require.NoError(t, r.Chunk("1", "tab", 0, 0, 0, false, []byte("a")), "a retried chunk is acknowledged")
	require.NoError(t, r.Chunk("1", "tab", 0, 1, 0, false, []byte("b")))
	assert.ErrorIs(t, r.Chunk("1", "tab", 0, 3, 0, false, []byte("d")), ErrOutOfOrder)

	data, err := os.ReadFile(filepath.Join(r.records["1"].dir, "seg-0.webm"))
	require.NoError(t, err)
	assert.Equal(t, "ab", string(data), "the retry must not be appended twice")
}

func TestStop_WithoutAudioFails(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	require.NoError(t, r.Start("1", 1000))
	_, err := r.Stop("1")
	assert.ErrorIs(t, err, ErrNoAudio)
	recording, _ := r.Status("1", "tab")
	assert.False(t, recording)
}

func TestChunk_OneTabCapturesAGame(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	clock := time.Unix(1000, 0)
	r.now = func() time.Time { return clock }
	require.NoError(t, r.Start("1", 0))
	t.Cleanup(func() { _, _ = r.Stop("1") })

	elsewhere := func(client string) bool {
		recording, e := r.Status("1", client)
		require.True(t, recording)
		return e
	}
	assert.False(t, elsewhere("gm"), "nobody captures yet")

	require.NoError(t, r.Chunk("1", "gm", 0, 0, 0, false, []byte("a")))
	assert.False(t, elsewhere("gm"), "the owner is not told to stay away")
	assert.True(t, elsewhere("co-gm"))
	assert.ErrorIs(t, r.Chunk("1", "co-gm", 1, 0, 0, false, []byte("x")), ErrCapturedElsewhere)

	// The owner keeps the capture as long as its chunks keep coming
	clock = clock.Add(captureLease - time.Second)
	require.NoError(t, r.Chunk("1", "gm", 0, 1, 0, false, []byte("b")))
	clock = clock.Add(captureLease - time.Second)
	assert.ErrorIs(t, r.Chunk("1", "co-gm", 1, 0, 0, false, []byte("x")), ErrCapturedElsewhere)

	// Its tab is gone: another one takes over, and it is refused if it comes back
	clock = clock.Add(2 * time.Second)
	assert.False(t, elsewhere("co-gm"))
	require.NoError(t, r.Chunk("1", "co-gm", 1, 0, 0, false, []byte("x")))
	assert.ErrorIs(t, r.Chunk("1", "gm", 0, 2, 0, false, []byte("c")), ErrCapturedElsewhere)

	// The refused chunks were not written
	data, err := os.ReadFile(filepath.Join(r.records["1"].dir, "seg-0.webm"))
	require.NoError(t, err)
	assert.Equal(t, "ab", string(data))
}

// Two games recorded at once, each by its own GM. The mix of a 4h session
// takes minutes: while one game's is running, the other must go on recording
func TestTwoGames_AreRecordedSideBySide(t *testing.T) {
	mixing, finish := make(chan struct{}), make(chan struct{})
	var mixes atomic.Int32
	mix := func(args []string) error {
		// Only the first mix (game A's) is slow
		if mixes.Add(1) == 1 {
			close(mixing)
			<-finish
		}
		return os.WriteFile(args[len(args)-1], []byte("ogg"), 0o644)
	}
	up := &keysUploader{}
	r := NewRecorder(t.TempDir(), up, mix)
	require.NoError(t, r.Start("A", 0))
	require.NoError(t, r.Start("B", 0))

	// Each GM holds their own game's capture, not the other's
	require.NoError(t, r.Chunk("A", "gm-a", 0, 0, 0, false, []byte("a0")))
	require.NoError(t, r.Chunk("B", "gm-b", 0, 0, 0, false, []byte("b0")))
	require.NoError(t, r.Chunk("A", "gm-a", 0, 1, 0, false, []byte("a1")))
	for game, want := range map[string]string{"A": "a0a1", "B": "b0"} {
		data, err := os.ReadFile(filepath.Join(r.records[game].dir, "seg-0.webm"))
		require.NoError(t, err)
		assert.Equal(t, want, string(data), "game %s holds the other game's audio", game)
	}

	// A's session ends and its mix starts...
	stopped := make(chan string)
	go func() {
		key, err := r.Stop("A")
		assert.NoError(t, err)
		stopped <- key
	}()
	<-mixing
	// ...B is still recorded meanwhile
	accepted := make(chan error)
	go func() { accepted <- r.Chunk("B", "gm-b", 0, 1, 0, false, []byte("b1")) }()
	select {
	case err := <-accepted:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("game B's chunk waited for game A's mix")
	}
	recording, _ := r.Status("B", "gm-b")
	assert.True(t, recording)
	recording, _ = r.Status("A", "gm-a")
	assert.False(t, recording, "A's GM is told to stop as soon as its session ends")

	close(finish)
	assert.Equal(t, "A.ogg", <-stopped)
	key, err := r.Stop("B")
	require.NoError(t, err)
	assert.Equal(t, "B.ogg", key)
	assert.Equal(t, []string{"A.ogg", "B.ogg"}, up.keys)
}

func TestMixArgs_PlacesSegments(t *testing.T) {
	args := mixArgs([]placedSegment{
		{path: "b", n: 1, startedAt: 900},
		{path: "a", n: 0, startedAt: 1500},
	}, 1000, "out.ogg")
	assert.Contains(t, args, "[0:a]adelay=500:all=1[a0];[1:a]atrim=start=0.100,asetpts=PTS-STARTPTS[a1];[a0][a1]amix=inputs=2:normalize=0:duration=longest[out]")
	assert.Equal(t, []string{"-i", "a"}, args[3:5], "segments are mixed in order")
}

// Runs the real ffmpeg: two 1s tones, the first starting 0.5s after Discord,
// the second after a 1s gap. The result must be silent and loud exactly there
func TestStop_AlignsSegmentsOnDiscordTimeline(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	tmp := t.TempDir()
	tone := func(name string) []byte {
		p := filepath.Join(tmp, name)
		out, err := exec.Command("ffmpeg", "-hide_banner", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
			"-ac", "2", "-c:a", "libopus", p).CombinedOutput()
		require.NoError(t, err, string(out))
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		return data
	}

	up := &copyUploader{dst: filepath.Join(tmp, "result.ogg")}
	r := NewRecorder(filepath.Join(tmp, "rec"), up, FFmpeg)
	require.NoError(t, r.Start("42", 10_000))
	// ffmpeg's sine plays at 1/8 amplitude (~-21 dB after opus): loud means > -30 dB,
	// silent < -50 dB
	// MediaRecorder chunks are arbitrary byte splits of one webm stream
	for i, seg := range [][]byte{tone("a.webm"), tone("b.webm")} {
		half := len(seg) / 2
		startedAt := []int64{10_500, 12_500}[i]
		require.NoError(t, r.Chunk("42", "tab", i, 0, startedAt, false, seg[:half]))
		require.NoError(t, r.Chunk("42", "tab", i, 1, startedAt, false, seg[half:]))
	}

	key, err := r.Stop("42")
	require.NoError(t, err)
	assert.Equal(t, "42.ogg", key)
	_, err = os.Stat(filepath.Join(tmp, "rec", "42"))
	assert.True(t, os.IsNotExist(err), "the working dir is cleaned up")

	maxDb := func(from, dur float64) float64 {
		out, _ := exec.Command("ffmpeg", "-hide_banner", "-ss", strconv.FormatFloat(from, 'f', 2, 64),
			"-t", strconv.FormatFloat(dur, 'f', 2, 64), "-i", up.dst, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
		m := regexp.MustCompile(`max_volume: (-?[\d.]+|-inf) dB`).FindStringSubmatch(string(out))
		require.NotNil(t, m, string(out))
		if m[1] == "-inf" {
			return -1000
		}
		v, _ := strconv.ParseFloat(m[1], 64)
		return v
	}
	assert.Less(t, maxDb(0, 0.4), -50.0, "silent before the first segment")
	assert.Greater(t, maxDb(0.6, 0.8), -30.0, "first tone at 0.5s")
	assert.Less(t, maxDb(1.6, 0.8), -50.0, "silent in the gap")
	assert.Greater(t, maxDb(2.6, 0.8), -30.0, "second tone at 2.5s")
}

func TestPictureArgs_OneTrackPerSegmentWithAPicture(t *testing.T) {
	assert.Nil(t, pictureArgs([]placedSegment{{path: "a", startedAt: 1500}}, 1000, "mix.ogg", "out.mkv"),
		"the jukebox alone stays an ogg")

	args := pictureArgs([]placedSegment{
		{path: "a", startedAt: 1500, video: true},
		{path: "b", startedAt: 2000},
		{path: "c", startedAt: 900, video: true},
	}, 1000, "mix.ogg", "out.mkv")
	assert.Equal(t, []string{"-hide_banner", "-nostdin", "-y", "-i", "mix.ogg",
		"-itsoffset", "0.500", "-i", "a",
		// Started before Discord: at 0
		"-itsoffset", "0.000", "-i", "c",
		"-map", "0:a", "-map", "1:v:0?", "-map", "2:v:0?", "-c", "copy", "out.mkv"}, args)
}

// Runs the real ffmpeg: a segment with the tab's picture from 0.5s, and one
// without from 2.5s. The jukebox mix goes with the picture, which keeps its
// place on the Discord timeline
func TestStop_KeepsThePictureOnDiscordTimeline(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	tmp := t.TempDir()
	record := func(name string, picture bool) []byte {
		p := filepath.Join(tmp, name)
		args := []string{"-hide_banner", "-y", "-f", "lavfi", "-i", "sine=duration=1"}
		if picture {
			args = append(args, "-f", "lavfi", "-i", "testsrc=s=320x240:r=15:d=1", "-c:v", "libvpx")
		}
		out, err := exec.Command("ffmpeg", append(args, "-c:a", "libopus", p)...).CombinedOutput()
		require.NoError(t, err, string(out))
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		return data
	}

	up := &copyUploader{dst: filepath.Join(tmp, "result.mkv")}
	r := NewRecorder(filepath.Join(tmp, "rec"), up, FFmpeg)
	require.NoError(t, r.Start("42", 10_000))
	require.NoError(t, r.Chunk("42", "tab", 0, 0, 10_500, true, record("a.webm", true)))
	require.NoError(t, r.Chunk("42", "tab", 1, 0, 12_500, false, record("b.webm", false)))

	key, err := r.Stop("42")
	require.NoError(t, err)
	assert.Equal(t, "video/42.mkv", key)

	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type,start_time",
		"-of", "csv=p=0", up.dst).Output()
	require.NoError(t, err)
	streams := strings.Fields(string(out))
	require.Len(t, streams, 2, string(out))
	assert.True(t, strings.HasPrefix(streams[0], "audio,"), "the jukebox mix")
	assert.Equal(t, "video", strings.Split(streams[1], ",")[0])
	start, _ := strconv.ParseFloat(strings.Split(streams[1], ",")[1], 64)
	assert.InDelta(t, 0.5, start, 0.05, "the picture starts where its segment did")
}

// Adding the picture failed: the GM still gets the music
func TestStop_FallsBackToTheJukeboxAlone(t *testing.T) {
	mix := func(args []string) error {
		if out := args[len(args)-1]; !strings.HasSuffix(out, ".mkv") {
			return os.WriteFile(out, []byte("ogg"), 0o644)
		}
		return errors.New("ffmpeg: broken picture")
	}
	up := &keysUploader{}
	r := NewRecorder(t.TempDir(), up, mix)
	require.NoError(t, r.Start("42", 0))
	require.NoError(t, r.Chunk("42", "tab", 0, 0, 0, true, []byte("webm")))
	key, err := r.Stop("42")
	require.NoError(t, err)
	assert.Equal(t, "42.ogg", key)
	assert.Equal(t, []string{"42.ogg"}, up.keys)
}
