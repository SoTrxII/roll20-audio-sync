# Roll20 Audio Recorder

Records the Roll20 jukebox of a game, aligned with its Discord recording.

The audio is captured in the GM's browser by Lullaby (tab capture, so it is exactly
what the GM hears) and sent here in 5-second chunks. On stop, this service places
every captured segment on the Discord recording's timeline, mixes them into one ogg
and uploads it to the object store as `<roll20 game id>.ogg`, which the video
pipeline blends in as background music.

> This replaced a reconstruction where the browser reported the jukebox *state* and
> [live-audio-mixer](https://github.com/SoTrxII/live-audio-mixer) re-downloaded and
> re-mixed every track. That broke on every Roll20 or source change.

## API

| Call | By | Description |
|---|---|---|
| `POST /v1/jukeboxsyncer/start` `{"id", "alignTo"}` | record-orchestrator | Start a record. `alignTo` (unix ms) is the Discord recording's t=0 |
| `POST /v1/jukeboxsyncer/stop` `{"id"}` | record-orchestrator | Mix, upload, answer `{"key"}`. 404 if nothing was captured |
| `GET /v1/time` | Lullaby | Server clock, to put segment start times on it |
| `GET /v1/capture/:id/status` | Lullaby | `{"recording": bool}` |
| `POST /v1/capture/:id/chunk?seg=&seq=&startedAt=` | Lullaby | One MediaRecorder chunk. 409: no recording, stop. 400: sequence broken, start a new segment |

`id` is the Roll20 game id. Chunks of a segment must arrive in order starting at
`seq=0` (it holds the webm header); a retried chunk is acknowledged without being
appended twice.

## Running locally

Prerequisites: Go >= 1.21, ffmpeg, [Dapr](https://dapr.io/), Docker.

```bash
# Starts a local MinIO with a "recordings" bucket if needed, then the app
# with its sidecar (object-store binding in .dapr/resources)
./deploy/start.sh
```

## Configuration

| Variable | Description | Default |
|---|---|---|
| `APP_PORT` | Port the app listens on | `8080` |
| `OBJECT_STORE_NAME` | Dapr binding to upload to | `object-store` |
| `OBJECT_STORE_B64` | Whether that binding expects base64 (`decodeBase64`) | `true` |
| `DAPR_GRPC_PORT` | Set by Dapr | `50001` |
