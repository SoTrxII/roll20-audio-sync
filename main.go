package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"log/slog"
	"os"
	"roll20-audio-bouncer/controller"
	mixer_client "roll20-audio-bouncer/internal/mixer-client"
	jukebox_syncer "roll20-audio-bouncer/service/jukebox-syncer"
	"strconv"
	"time"
)

const (
	GIN_MODE = "GIN_MODE"
	// Dapr id for the remote mixer
	DEFAULT_MIXER_DID = "live-audio-mixer"
	DEFAULT_APP_PORT  = 8080
)

func main() {
	// Env is loaded after gin is initialized, we must set it manually
	if os.Getenv(GIN_MODE) == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Dapr client
	daprPort := 50001
	if envPort, err := strconv.ParseInt(os.Getenv("DAPR_GRPC_PORT"), 10, 32); err == nil && envPort != 0 {
		daprPort = int(envPort)
	}
	slog.Info("[Main] :: Dapr port is " + strconv.Itoa(daprPort))
	daprMixerId := os.Getenv("MIXER_APP_ID")
	if daprMixerId == "" {
		daprMixerId = DEFAULT_MIXER_DID
	}
	appPort := DEFAULT_APP_PORT
	if envPort, err := strconv.ParseInt(os.Getenv("APP_PORT"), 10, 32); err == nil && envPort != 0 {
		appPort = int(envPort)
	}

	// Initialize controllers
	evtCtrl, err := DI(fmt.Sprintf("localhost:%d", daprPort), daprMixerId)
	if err != nil {
		panic(fmt.Errorf("failed to initialize event controller: %w", err))
	}
	router := gin.Default()
	router.Use(func() gin.HandlerFunc {
		return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
			return fmt.Sprintf(`%s - [%s] "%s %s %s %d %s "%s" %s"`,
				param.ClientIP,
				param.TimeStamp.Format(time.RFC1123),
				param.Method,
				param.Path,
				param.Request.Proto,
				param.StatusCode,
				param.Latency,
				param.Request.UserAgent(),
				param.ErrorMessage,
			)
		})
	}())

	// Define all routes
	v1 := router.Group("/v1")
	{
		evt := v1.Group("/jukeboxsyncer")
		{
			evt.POST("/start", evtCtrl.Start)
			evt.POST("/stop", evtCtrl.Stop)
			evt.POST("/evt", evtCtrl.Handle)
		}
	}
	slog.Info(fmt.Sprintf("[Main] :: Starting server on port %d", appPort))
	err = router.Run(fmt.Sprintf(":%d", appPort))
	if err != nil {
		log.Fatalf(err.Error())
	}
}

func DI(daprAddress, mixerId string) (*controller.EventController, error) {
	mixerApi, err := mixer_client.NewMixerClient(daprAddress, mixerId)
	if err != nil {
		return nil, err
	}
	syncer := jukebox_syncer.NewJukeboxSyncer(mixerApi)
	return controller.NewEventController(syncer), nil
}
