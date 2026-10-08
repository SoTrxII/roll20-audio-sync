.PHONY: dapr_run dapr test

dapr_run:
	dapr run --app-id=roll20-audio-sync --app-port 8080 --dapr-grpc-port 50001 --resources-path ./.dapr/resources -- go run main.go

dapr:
	dapr run --app-id=roll20-audio-sync --app-port 8080 --dapr-grpc-port 50001 --resources-path ./.dapr/resources
test:
	go test -v ./... -covermode=atomic -coverprofile=coverage.out
