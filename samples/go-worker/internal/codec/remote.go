package codec

import (
	"context"
	"fmt"
	"log"

	commonpb "go.temporal.io/api/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	pb "github.com/vsemashko/large-files-temporal/samples/go-worker/pkg/proto"
)

// RemotePayloadCodec implements a remote codec that communicates with the codec server via gRPC
type RemotePayloadCodec struct {
	client pb.PayloadCodecClient
	conn   *grpc.ClientConn
}

// NewRemotePayloadCodec creates a new remote codec
func NewRemotePayloadCodec(codecServerAddr string) (*RemotePayloadCodec, error) {
	// Connect to codec server
	conn, err := grpc.Dial(codecServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to codec server: %w", err)
	}

	client := pb.NewPayloadCodecClient(conn)

	return &RemotePayloadCodec{
		client: client,
		conn:   conn,
	}, nil
}

// Encode encodes payloads by calling the remote codec server
func (r *RemotePayloadCodec) Encode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	if len(payloads) == 0 {
		return payloads, nil
	}

	// Serialize payloads to bytes
	payloadBytes := make([][]byte, len(payloads))
	for i, p := range payloads {
		data, err := proto.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload %d: %w", i, err)
		}
		payloadBytes[i] = data
	}

	// Call remote codec server
	// Note: For simplicity, we're not passing workflow context here
	// In production, you might want to extract this from the payload metadata
	resp, err := r.client.Encode(context.Background(), &pb.EncodeRequest{
		Payloads:   payloadBytes,
		Namespace:  "default",
		WorkflowId: "unknown", // Will be set by interceptor in production
		RunId:      "unknown",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode payloads: %w", err)
	}

	// Deserialize response
	encoded := make([]*commonpb.Payload, len(resp.EncodedPayloads))
	for i, data := range resp.EncodedPayloads {
		p := &commonpb.Payload{}
		if err := proto.Unmarshal(data, p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal encoded payload %d: %w", i, err)
		}
		encoded[i] = p
	}

	log.Printf("Encoded %d payloads via remote codec", len(encoded))
	return encoded, nil
}

// Decode decodes payloads by calling the remote codec server
func (r *RemotePayloadCodec) Decode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	if len(payloads) == 0 {
		return payloads, nil
	}

	// Serialize payloads to bytes
	payloadBytes := make([][]byte, len(payloads))
	for i, p := range payloads {
		data, err := proto.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload %d: %w", i, err)
		}
		payloadBytes[i] = data
	}

	// Call remote codec server
	resp, err := r.client.Decode(context.Background(), &pb.DecodeRequest{
		Payloads: payloadBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode payloads: %w", err)
	}

	// Deserialize response
	decoded := make([]*commonpb.Payload, len(resp.DecodedPayloads))
	for i, data := range resp.DecodedPayloads {
		p := &commonpb.Payload{}
		if err := proto.Unmarshal(data, p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal decoded payload %d: %w", i, err)
		}
		decoded[i] = p
	}

	log.Printf("Decoded %d payloads via remote codec", len(decoded))
	return decoded, nil
}

// Close closes the connection to the codec server
func (r *RemotePayloadCodec) Close() error {
	return r.conn.Close()
}
