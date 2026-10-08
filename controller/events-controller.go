package controller

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"roll20-audio-bouncer/service/capture"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// A MediaRecorder chunk is 5s of opus, ~80 KB. 10 MB leaves room for a
// stalled tab flushing a backlog in one go, and caps what a stranger can push
const maxChunkBytes = 10 << 20

type Capturer interface {
	Start(id string, alignTo int64) error
	IsRecording(id string) bool
	Chunk(id string, seg, seq int, startedAt int64, data []byte) error
	Stop(id string) (string, error)
}

type CaptureController struct {
	rec Capturer
}

func NewCaptureController(rec Capturer) *CaptureController {
	return &CaptureController{rec: rec}
}

// Sent by record-orchestrator on /session start
type startPayload struct {
	Id string `json:"id" binding:"required"`
	// Unix ms of the Discord recording's t=0. Optional: without it the
	// capture's first segment is t=0
	AlignTo int64 `json:"alignTo"`
}

type stopPayload struct {
	Id string `json:"id" binding:"required"`
}

func (cc *CaptureController) Start(c *gin.Context) {
	var p startPayload
	if err := c.BindJSON(&p); err != nil {
		return
	}
	switch err := cc.rec.Start(p.Id, p.AlignTo); {
	case errors.Is(err, capture.ErrAlreadyRecording):
		c.String(http.StatusConflict, err.Error())
	case err != nil:
		slog.Error(fmt.Sprintf("[Capture controller] :: starting %s : %v", p.Id, err))
		c.String(http.StatusInternalServerError, err.Error())
	default:
		slog.Info(fmt.Sprintf("[Capture controller] :: recording %s, aligned to %d", p.Id, p.AlignTo))
		c.Status(http.StatusAccepted)
	}
}

// Stop mixes and uploads before answering: record-orchestrator hands the key
// on to the video pipeline right away, so the object must exist by then
func (cc *CaptureController) Stop(c *gin.Context) {
	var p stopPayload
	if err := c.BindJSON(&p); err != nil {
		return
	}
	key, err := cc.rec.Stop(p.Id)
	switch {
	case errors.Is(err, capture.ErrNotRecording), errors.Is(err, capture.ErrNoAudio):
		// record-orchestrator carries on without background audio
		slog.Warn(fmt.Sprintf("[Capture controller] :: stopping %s : %v", p.Id, err))
		c.String(http.StatusNotFound, err.Error())
	case err != nil:
		slog.Error(fmt.Sprintf("[Capture controller] :: stopping %s : %v", p.Id, err))
		c.String(http.StatusInternalServerError, err.Error())
	default:
		slog.Info(fmt.Sprintf("[Capture controller] :: %s uploaded as %s", p.Id, key))
		c.JSON(http.StatusOK, gin.H{"key": key})
	}
}

// Time lets Lullaby convert the GM's clock to ours: segment start times must
// be on the same clock as the alignTo record-orchestrator sends
func (cc *CaptureController) Time(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"now": time.Now().UnixMilli()})
}

func (cc *CaptureController) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"recording": cc.rec.IsRecording(c.Param("id"))})
}

// Chunk answers 409 when there's no recording (never started, or /session end
// came first): Lullaby stops capturing. 400 is a sequence it can't recover in
// the same segment: it starts a new one
func (cc *CaptureController) Chunk(c *gin.Context) {
	id := c.Param("id")
	seg, err1 := strconv.Atoi(c.Query("seg"))
	seq, err2 := strconv.Atoi(c.Query("seq"))
	startedAt, err3 := strconv.ParseInt(c.Query("startedAt"), 10, 64)
	if err := errors.Join(err1, err2, err3); err != nil || seg < 0 || seq < 0 {
		c.String(http.StatusBadRequest, "seg, seq and startedAt must be non-negative integers")
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxChunkBytes))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		c.String(http.StatusRequestEntityTooLarge, err.Error())
		return
	} else if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	switch err := cc.rec.Chunk(id, seg, seq, startedAt, data); {
	case errors.Is(err, capture.ErrNotRecording):
		c.String(http.StatusConflict, err.Error())
	case errors.Is(err, capture.ErrOutOfOrder):
		c.String(http.StatusBadRequest, err.Error())
	case err != nil:
		slog.Error(fmt.Sprintf("[Capture controller] :: chunk %d/%d of %s : %v", seg, seq, id, err))
		c.String(http.StatusInternalServerError, err.Error())
	default:
		c.Status(http.StatusNoContent)
	}
}
