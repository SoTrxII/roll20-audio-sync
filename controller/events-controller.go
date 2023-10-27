package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"roll20-audio-bouncer/service/jukebox-syncer"
)

type StateHandler interface {
	Handle(r *jukebox_syncer.R20State) error
	Start(id string) error
	Stop(id string) error
}
type EventController struct {
	syncer StateHandler
}

func NewEventController(syncer StateHandler) *EventController {
	return &EventController{
		syncer: syncer,
	}
}
func (ec *EventController) Start(c *gin.Context) {
	var target jukebox_syncer.RecPayload

	if err := c.BindJSON(&target); err != nil {
		slog.Info(fmt.Sprintf("[evt controller] :: invalid body provided: %s !", err.Error()))
		c.String(http.StatusBadRequest, `invalid body provided: %s !`, err.Error())
		return
	}

	if err := ec.syncer.Start(target.Id); err != nil {
		slog.Error(fmt.Sprintf("[evt controller] :: while starting an new record with id %s : %s", target.Id, err))
		c.String(http.StatusInternalServerError, err.Error())
		return
	} else {
		c.String(http.StatusAccepted, "")
	}
	slog.Info(fmt.Sprintf("[evt controller] :: starting an new record with id %s", target.Id))
}

func (ec *EventController) Stop(c *gin.Context) {
	var target jukebox_syncer.RecPayload

	if err := c.BindJSON(&target); err != nil {
		slog.Info(fmt.Sprintf("[evt controller] :: invalid body provided: %s !", err.Error()))
		c.String(http.StatusBadRequest, `invalid body provided: %s !`, err.Error())
		return
	}

	if err := ec.syncer.Stop(target.Id); err != nil {
		slog.Error(fmt.Sprintf("[evt controller] :: while stopping existing record with id %s : %s", target.Id, err))
		c.String(http.StatusInternalServerError, err.Error())
		return
	} else {
		c.String(http.StatusAccepted, "")
	}
	slog.Info(fmt.Sprintf("[evt controller] :: stopping existing record with id %s", target.Id))
}

func (ec *EventController) Handle(c *gin.Context) {
	var target jukebox_syncer.R20State

	if err := c.BindJSON(&target); err != nil {
		slog.Info(fmt.Sprintf("[evt controller] :: invalid body provided: %s !", err.Error()))
		c.String(http.StatusBadRequest, `invalid body provided: %s !`, err.Error())
		return
	}
	slog.Info(fmt.Sprintf("[evt controller] :: processing %+v", target))
	if err := ec.syncer.Handle(&target); err != nil {
		slog.Error(fmt.Sprintf("[evt controller] :: while processing %v : %s", target, err))
	}
	c.String(http.StatusAccepted, "")
}
