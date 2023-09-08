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
	client pb.EventStreamClient
	ctx    context.Context
	stream pb.EventStream_StreamEventsClient
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
	//_ = client
	return &MixerClient{client: client, ctx: ctx}, nil
}

func (mc *MixerClient) Start(id string) error {
	_, err := mc.client.Start(mc.ctx, &pb.RecordRequest{Id: id})
	if err != nil {
		return err
	}
	// Also start the event stream
	if mc.stream == nil {
		mc.stream, err = mc.client.StreamEvents(mc.ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (mc *MixerClient) Stop(id string) error {
	_, err := mc.client.Stop(mc.ctx, &pb.StopRequest{Id: id})
	if err != nil {
		return err
	}
	if mc.stream != nil {
		err = mc.stream.CloseSend()
		if err != nil {
			return err
		}
	}
	return nil
}

func (mc *MixerClient) initStream() (pb.EventStream_StreamEventsClient, error) {
	stream, err := mc.client.StreamEvents(mc.ctx)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (mc *MixerClient) Send(evt *pb.Event) error {
	var err error
	if mc.stream == nil {
		mc.stream, err = mc.client.StreamEvents(mc.ctx)
		if err != nil {
			return err
		}
	}
	err = mc.stream.Send(evt)
	if err != nil {
		return err
	}
	return nil
}
