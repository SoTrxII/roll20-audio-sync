package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"net/http"
	"net/http/httptest"
	jukebox_syncer "roll20-audio-bouncer/service/jukebox-syncer"
	"testing"
	"time"
)

var (
	sampleState = jukebox_syncer.R20State{
		Rid:  "1",
		Uid:  "2",
		Date: time.Now(),
		Tracks: []jukebox_syncer.R20Track{
			{
				Url:     "a",
				Playing: true,
			},
		},
	}
	sampleRecPayload = jukebox_syncer.RecPayload{
		Id: "1",
	}
)

func TestEventController_HandleBadRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctrl.Handle(c)
	mockHandler.AssertNotCalled(t, "Handle", mock.Anything)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEventController_HandleOkRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Handle", mock.Anything).Return(nil)
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleState)
	ctrl.Handle(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestEventController_HandleError(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Handle", mock.Anything).Return(fmt.Errorf("Test"))
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleState)
	ctrl.Handle(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestEventController_StartBadRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctrl.Start(c)
	mockHandler.AssertNotCalled(t, "Start", mock.Anything)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEventController_StartOkRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Start", mock.Anything).Return(nil)
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleRecPayload)
	ctrl.Start(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestEventController_StartError(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Start", mock.Anything).Return(fmt.Errorf("Test"))
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleRecPayload)
	ctrl.Start(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestEventController_StopBadRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	ctrl.Stop(c)
	mockHandler.AssertNotCalled(t, "Stop", mock.Anything)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestEventController_StopOkRequest(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Stop", mock.Anything).Return(nil)
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleRecPayload)
	ctrl.Stop(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestEventController_StopError(t *testing.T) {
	mockHandler := mockStateHandler{}
	mockHandler.On("Stop", mock.Anything).Return(fmt.Errorf("Test"))
	ctrl := NewEventController(&mockHandler)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setJsonAsBody(t, c, sampleRecPayload)
	ctrl.Stop(c)
	mockHandler.AssertExpectations(t)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Set the payload as the JSON body of c
func setJsonAsBody(t *testing.T, c *gin.Context, payload any) {
	buf, err := json.Marshal(payload)
	if err != nil {
		fmt.Println(err)
		t.Fatal(err)
	}
	c.Request, _ = http.NewRequest("POST", "/", bytes.NewBuffer(buf))
	c.Request.Header.Set("Content-Type", "application/json")
}

type mockStateHandler struct {
	mock.Mock
}

func (m *mockStateHandler) Handle(r *jukebox_syncer.R20State) error {
	args := m.Called(r)
	return args.Error(0)
}

func (m *mockStateHandler) Start(id string) error {
	args := m.Called(id)
	return args.Error(0)
}
func (m *mockStateHandler) Stop(id string) error {
	args := m.Called(id)
	return args.Error(0)
}
