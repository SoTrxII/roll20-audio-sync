package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"roll20-audio-bouncer/service/capture"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type fakeCapturer struct {
	startErr, chunkErr, stopErr error
	gotAlignTo                  int64
	gotChunk                    []byte
	gotClient                   string
	gotVideo                    bool
}

func (f *fakeCapturer) Start(_ string, alignTo int64) error {
	f.gotAlignTo = alignTo
	return f.startErr
}
func (f *fakeCapturer) Status(_, client string) (bool, bool) { return true, client != "owner" }
func (f *fakeCapturer) Chunk(_, client string, _, _ int, _ int64, video bool, data []byte) error {
	f.gotChunk, f.gotClient, f.gotVideo = data, client, video
	return f.chunkErr
}
func (f *fakeCapturer) Stop(id string) (string, error) { return id + ".ogg", f.stopErr }

func serve(f *fakeCapturer, method, url string, body []byte) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cc := NewCaptureController(f)
	r.POST("/start", cc.Start)
	r.POST("/stop", cc.Stop)
	r.POST("/chunk/:id", cc.Chunk)
	r.GET("/status/:id", cc.Status)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, url, bytes.NewReader(body)))
	return w
}

func TestStart_PassesAlignmentAndRefusesDuplicates(t *testing.T) {
	f := &fakeCapturer{}
	assert.Equal(t, http.StatusAccepted, serve(f, "POST", "/start", []byte(`{"id":"1","alignTo":1234}`)).Code)
	assert.Equal(t, int64(1234), f.gotAlignTo)

	// record-orchestrator before alignTo existed
	assert.Equal(t, http.StatusAccepted, serve(f, "POST", "/start", []byte(`{"id":"1"}`)).Code)

	f.startErr = capture.ErrAlreadyRecording
	assert.Equal(t, http.StatusConflict, serve(f, "POST", "/start", []byte(`{"id":"1"}`)).Code)
	assert.Equal(t, http.StatusBadRequest, serve(f, "POST", "/start", []byte(`{}`)).Code)
}

func TestStop_NothingCapturedIsNotFound(t *testing.T) {
	f := &fakeCapturer{}
	w := serve(f, "POST", "/stop", []byte(`{"id":"1"}`))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"key":"1.ogg"}`, w.Body.String())

	f.stopErr = capture.ErrNoAudio
	assert.Equal(t, http.StatusNotFound, serve(f, "POST", "/stop", []byte(`{"id":"1"}`)).Code)
}

func TestChunk_StatusTellsLullabyWhatToDo(t *testing.T) {
	const ok = "/chunk/1?seg=0&seq=0&startedAt=5"
	f := &fakeCapturer{}
	assert.Equal(t, http.StatusNoContent, serve(f, "POST", ok, []byte("opus")).Code)
	assert.Equal(t, "opus", string(f.gotChunk))
	assert.False(t, f.gotVideo, "a Lullaby that does not say records the jukebox alone")
	serve(f, "POST", ok+"&video=1", []byte("webm"))
	assert.True(t, f.gotVideo)

	for _, bad := range []string{"/chunk/1?seq=0&startedAt=5", "/chunk/1?seg=-1&seq=0&startedAt=5", "/chunk/1?seg=0&seq=x&startedAt=5"} {
		assert.Equal(t, http.StatusBadRequest, serve(f, "POST", bad, nil).Code, bad)
	}
	assert.Equal(t, http.StatusRequestEntityTooLarge, serve(f, "POST", ok, make([]byte, maxChunkBytes+1)).Code)

	f.chunkErr = capture.ErrNotRecording
	assert.Equal(t, http.StatusConflict, serve(f, "POST", ok, nil).Code, "stop capturing")
	f.chunkErr = capture.ErrOutOfOrder
	assert.Equal(t, http.StatusBadRequest, serve(f, "POST", ok, nil).Code, "start a new segment")
	f.chunkErr = capture.ErrCapturedElsewhere
	assert.Equal(t, http.StatusLocked, serve(f, "POST", ok+"&client=co-gm", nil).Code, "another tab captures: stop and say so")
	assert.Equal(t, "co-gm", f.gotClient)
}

func TestStatus_TellsATabWhetherAnotherOneCaptures(t *testing.T) {
	f := &fakeCapturer{}
	assert.JSONEq(t, `{"recording":true,"capturedElsewhere":false}`, serve(f, "GET", "/status/1?client=owner", nil).Body.String())
	assert.JSONEq(t, `{"recording":true,"capturedElsewhere":true}`, serve(f, "GET", "/status/1?client=co-gm", nil).Body.String())
}
