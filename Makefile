.PHONY: run dapr test proto

dapr_run:
	dapr run  --app-id=r20-audio-bouncer --app-port 50302 --dapr-grpc-port 50001 --resources-path ./.dapr/resources -- go run main.go

dapr:
	dapr run --app-id=r20-audio-bouncer --app-port 50302 --dapr-grpc-port 50001  --resources-path ./.dapr/resources
test:
	go test -v ./... -covermode=atomic -coverprofile=coverage.out

proto:
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/events.proto
