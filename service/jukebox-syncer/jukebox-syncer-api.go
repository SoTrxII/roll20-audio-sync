package jukebox_syncer

import (
	pb "roll20-audio-bouncer/proto"
	"time"
)

// Events as received by roll20
type R20Track struct {
	Title   string  `json:"title" omitempty:"true"`
	Url     string  `json:"url" binding:"required"`
	Loop    bool    `json:"loop" binding:"required"`
	Playing bool    `json:"playing" binding:"required"`
	Volume  float64 `json:"volume" binding:"required"`
}

type R20State struct {
	Uid    string     `json:"uId" binding:"required"`
	Tracks []R20Track `json:"tracks" binding:"required"`
	Rid    string     `json:"rId" binding:"required"`
	Date   time.Time  `json:"date" binding:"required"`
}

// Required payload to start or stop a recording
type RecPayload struct {
	Id string `json:"id" binding:"required"`
}

// Backend API
type MixerAPI interface {
	Start(id string) error
	Stop(id string) error
	Send(evt *pb.Event) error
}
