# Roll20 Audio Recorder

Records the Roll20 jukebox of a game, aligned with its Discord recording.

The audio is captured in the GM's browser by Lullaby (tab capture, so it is exactly
what the GM hears) and sent here in 5-second chunks. On stop, this service places
every captured segment on the Discord recording's timeline, mixes them into one ogg
and uploads it to the object store as `<roll20 game id>.ogg`, which the video
pipeline blends in as background music.

When the GM also records the tab's picture, that mix goes out as
`video/<roll20 game id>.mkv` instead, with one picture track per segment, copied
as recorded and starting where the segment did on the Discord timeline. encode-box
lays them over its black background. The `video/` prefix expires after 30 days in
the bucket. Over ~1.8 GB, the most an object can weigh through Dapr, or if adding
the picture fails, the upload falls back to the `.ogg` alone.

> This replaced a reconstruction where the browser reported the jukebox *state* and
> [live-audio-mixer](https://github.com/SoTrxII/live-audio-mixer) re-downloaded and
> re-mixed every track. That broke on every Roll20 or source change.

## API

| Call | By | Description |
|---|---|---|
| `POST /v1/jukeboxsyncer/start` `{"id", "alignTo"}` | record-orchestrator | Start a record. `alignTo` (unix ms) is the Discord recording's t=0 |
| `POST /v1/jukeboxsyncer/stop` `{"id"}` | record-orchestrator | Mix, upload, answer `{"key"}` (`<id>.ogg` or `video/<id>.mkv`). 404 if nothing was captured |
| `GET /v1/time` | Lullaby | Server clock, to put segment start times on it |
| `GET /v1/capture/:id/status?client=` | Lullaby | `{"recording": bool, "capturedElsewhere": bool}`. The second is true when another tab than `client` captures this game |
| `POST /v1/capture/:id/chunk?seg=&seq=&startedAt=&client=&video=` | Lullaby | One MediaRecorder chunk, `video=1` when it holds the picture too (the segment's first chunk decides). 409: no recording, stop. 423: another tab captures this game, stop. 400: sequence broken, start a new segment |

One tab captures a game at a time: two GM tabs of the same game would both be mixed
in and double the music. The tab whose chunk arrives first holds the capture, each
chunk renews that for 30s, and another tab can only take over once it lapsed.
`client` is an id Lullaby keeps per tab, across reloads.

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
