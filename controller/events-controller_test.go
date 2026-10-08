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
}

func (f *fakeCapturer) Start(_ string, alignTo int64) error {
	f.gotAlignTo = alignTo
	return f.startErr
}
func (f *fakeCapturer) IsRecording(string) bool { return true }
func (f *fakeCapturer) Chunk(_ string, _, _ int, _ int64, data []byte) error {
	f.gotChunk = data
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

	for _, bad := range []string{"/chunk/1?seq=0&startedAt=5", "/chunk/1?seg=-1&seq=0&startedAt=5", "/chunk/1?seg=0&seq=x&startedAt=5"} {
		assert.Equal(t, http.StatusBadRequest, serve(f, "POST", bad, nil).Code, bad)
	}
	assert.Equal(t, http.StatusRequestEntityTooLarge, serve(f, "POST", ok, make([]byte, maxChunkBytes+1)).Code)

	f.chunkErr = capture.ErrNotRecording
	assert.Equal(t, http.StatusConflict, serve(f, "POST", ok, nil).Code, "stop capturing")
	f.chunkErr = capture.ErrOutOfOrder
	assert.Equal(t, http.StatusBadRequest, serve(f, "POST", ok, nil).Code, "start a new segment")
}
