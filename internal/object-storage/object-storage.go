// Package object_storage uploads through a Dapr S3 binding. Trimmed from
// live-audio-mixer's, which this service replaced: only Upload is needed
package object_storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"

	"github.com/dapr/go-sdk/client"
)

type BindingProxy interface {
	InvokeBinding(ctx context.Context, in *client.InvokeBindingRequest) (out *client.BindingEvent, err error)
}

type ObjectStorage struct {
	componentName string
	client        BindingProxy
	// The recordings bucket's binding sets decodeBase64: it expects base64,
	// not the raw bytes
	isBase64 bool
}

func NewObjectStorage(client BindingProxy, component string, base64 bool) *ObjectStorage {
	return &ObjectStorage{componentName: component, client: client, isBase64: base64}
}

// Upload sends the whole file in one binding call. A 4h session at 96k is
// ~170 MB (~230 MB in base64): the sidecar's max request size must allow it
func (od *ObjectStorage) Upload(path string, key string) error {
	data, err := od.read(path)
	if err != nil {
		return err
	}
	_, err = od.client.InvokeBinding(context.Background(), &client.InvokeBindingRequest{
		Name:      od.componentName,
		Operation: "create",
		Data:      data,
		Metadata: map[string]string{
			// The S3 binding reads "key", the local storage one "fileName"
			// https://docs.dapr.io/reference/components-reference/supported-bindings/s3/
			"key":      key,
			"fileName": key,
		},
	})
	return err
}

func (od *ObjectStorage) read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !od.isBase64 {
		return io.ReadAll(f)
	}
	var buf bytes.Buffer
	enc := base64.NewEncoder(base64.StdEncoding, &buf)
	if _, err := io.Copy(enc, f); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
