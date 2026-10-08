package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

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

func TestChunk_RefusesUnknownRecord(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	assert.ErrorIs(t, r.Chunk("1", 0, 0, 0, []byte("x")), ErrNotRecording)
}

func TestChunk_SequenceIsIdempotentAndOrdered(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	require.NoError(t, r.Start("1", 0))
	// Closes the segment file, which Windows won't let TempDir delete while open.
	// The upload fails (noMix wrote nothing), which is irrelevant here
	t.Cleanup(func() { _, _ = r.Stop("1") })

	assert.ErrorIs(t, r.Chunk("1", 0, 1, 0, []byte("b")), ErrOutOfOrder, "a segment must start with its header chunk")
	require.NoError(t, r.Chunk("1", 0, 0, 0, []byte("a")))
	require.NoError(t, r.Chunk("1", 0, 0, 0, []byte("a")), "a retried chunk is acknowledged")
	require.NoError(t, r.Chunk("1", 0, 1, 0, []byte("b")))
	assert.ErrorIs(t, r.Chunk("1", 0, 3, 0, []byte("d")), ErrOutOfOrder)

	data, err := os.ReadFile(filepath.Join(r.records["1"].dir, "seg-0.webm"))
	require.NoError(t, err)
	assert.Equal(t, "ab", string(data), "the retry must not be appended twice")
}

func TestStop_WithoutAudioFails(t *testing.T) {
	r := NewRecorder(t.TempDir(), &copyUploader{}, noMix)
	require.NoError(t, r.Start("1", 1000))
	_, err := r.Stop("1")
	assert.ErrorIs(t, err, ErrNoAudio)
	assert.False(t, r.IsRecording("1"))
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
		require.NoError(t, r.Chunk("42", i, 0, startedAt, seg[:half]))
		require.NoError(t, r.Chunk("42", i, 1, startedAt, seg[half:]))
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
