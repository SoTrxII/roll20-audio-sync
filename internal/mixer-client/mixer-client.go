package mixer_client

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"log"
	pb "roll20-audio-bouncer/proto"
	"time"
)

type MixerClient struct {
	client  pb.EventStreamClient
	ctx     context.Context
	streams map[string]pb.EventStream_StreamEventsClient
}

func NewMixerClient(address, daprMixerAppId string) (*MixerClient, error) {
	dialCtx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	conn, err := grpc.DialContext(dialCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "dapr-app-id", daprMixerAppId)
	ctx = metadata.AppendToOutgoingContext(ctx, "dapr-stream", "true")
	client := pb.NewEventStreamClient(conn)
	return &MixerClient{client: client, ctx: ctx, streams: map[string]pb.EventStream_StreamEventsClient{}}, nil
}

func (mc *MixerClient) Start(id string) error {
	_, err := mc.client.Start(mc.ctx, &pb.RecordRequest{Id: id})
	if err != nil {
		return err
	}

	// Create a new stream for this record
	_, ok := mc.streams[id]
	if !ok {
		stream, err := mc.client.StreamEvents(mc.ctx)
		if err != nil {
			return err
		}
		mc.streams[id] = stream
	}
	return nil
}

func (mc *MixerClient) Stop(id string) error {
	_, err := mc.client.Stop(mc.ctx, &pb.StopRequest{Id: id})
	if err != nil {
		return err
	}

	// Close this record stream
	stream, ok := mc.streams[id]
	if ok {
		err = stream.CloseSend()
		if err != nil {
			return err
		}
		delete(mc.streams, id)
	}
	return nil
}

func (mc *MixerClient) Send(evt *pb.Event) error {
	var err error
	stream, ok := mc.streams[evt.RecordId]
	if !ok {
		stream, err = mc.client.StreamEvents(mc.ctx)
		if err != nil {
			return err
		}
		mc.streams[evt.RecordId] = stream
	}
	err = stream.Send(evt)
	return err
}
