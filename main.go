package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"roll20-audio-bouncer/controller"
	object_storage "roll20-audio-bouncer/internal/object-storage"
	"roll20-audio-bouncer/service/capture"
	"strconv"
	"time"

	dapr "github.com/dapr/go-sdk/client"
	"github.com/gin-gonic/gin"
)

const (
	GIN_MODE           = "GIN_MODE"
	DEFAULT_APP_PORT   = 8080
	DEFAULT_STORE_NAME = "object-store"
	// The recordings bucket's binding sets decodeBase64: true
	DEFAULT_STORE_B64 = true
	// Segments are written here until /stop mixes and uploads them
	REC_DIR = "./rec"
)

func main() {
	// Env is loaded after gin is initialized, we must set it manually
	if os.Getenv(GIN_MODE) == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	appPort := DEFAULT_APP_PORT
	if envPort, err := strconv.ParseInt(os.Getenv("APP_PORT"), 10, 32); err == nil && envPort != 0 {
		appPort = int(envPort)
	}
	storeName := DEFAULT_STORE_NAME
	if name := os.Getenv("OBJECT_STORE_NAME"); name != "" {
		storeName = name
	}
	storeB64 := DEFAULT_STORE_B64
	if b64, err := strconv.ParseBool(os.Getenv("OBJECT_STORE_B64")); err == nil {
		storeB64 = b64
	}

	// Reads DAPR_GRPC_PORT, set by the sidecar injector
	daprClient, err := dapr.NewClient()
	if err != nil {
		panic(fmt.Errorf("failed to reach the Dapr sidecar: %w", err))
	}
	defer daprClient.Close()

	rec := capture.NewRecorder(REC_DIR, object_storage.NewObjectStorage(daprClient, storeName, storeB64), capture.FFmpeg)
	ctrl := controller.NewCaptureController(rec)

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

	v1 := router.Group("/v1")
	{
		// Called by record-orchestrator: the paths predate the capture and are kept for it
		jkb := v1.Group("/jukeboxsyncer")
		{
			jkb.POST("/start", ctrl.Start)
			jkb.POST("/stop", ctrl.Stop)
		}
		// Called by Lullaby, from the GM's browser
		v1.GET("/time", ctrl.Time)
		cpt := v1.Group("/capture/:id")
		{
			cpt.GET("/status", ctrl.Status)
			cpt.POST("/chunk", ctrl.Chunk)
		}
	}
	slog.Info(fmt.Sprintf("[Main] :: Starting server on port %d", appPort))
	if err := router.Run(fmt.Sprintf(":%d", appPort)); err != nil {
		log.Fatalf(err.Error())
	}
}
