package grpc

import (
	"context"
	"fmt"
	"log"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	pb "github.com/vsemashko/large-files-temporal/codec-server/pkg/proto"
	"go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

// Server implements the PayloadCodec gRPC service
type Server struct {
	pb.UnimplementedPayloadCodecServer
	codec *codec.Codec
}

// NewServer creates a new gRPC server
func NewServer(c *codec.Codec) *Server {
	return &Server{
		codec: c,
	}
}

// Encode encodes payloads
func (s *Server) Encode(ctx context.Context, req *pb.EncodeRequest) (*pb.EncodeResponse, error) {
	log.Printf("Encode request: namespace=%s, workflowID=%s, runID=%s, payloads=%d",
		req.Namespace, req.WorkflowId, req.RunId, len(req.Payloads))

	// Convert proto bytes to Temporal payloads
	payloads := make([]*common.Payload, len(req.Payloads))
	for i, data := range req.Payloads {
		payload := &common.Payload{}
		if err := proto.Unmarshal(data, payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload %d: %w", i, err)
		}
		payloads[i] = payload
	}

	// Encode payloads
	encoded, err := s.codec.Encode(ctx, payloads, req.Namespace, req.WorkflowId, req.RunId)
	if err != nil {
		log.Printf("Encode error: %v", err)
		return nil, fmt.Errorf("failed to encode payloads: %w", err)
	}

	// Convert back to proto bytes
	encodedBytes := make([][]byte, len(encoded))
	for i, payload := range encoded {
		data, err := proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal encoded payload %d: %w", i, err)
		}
		encodedBytes[i] = data
	}

	log.Printf("Encode response: %d payloads", len(encodedBytes))

	return &pb.EncodeResponse{
		EncodedPayloads: encodedBytes,
	}, nil
}

// Decode decodes payloads
func (s *Server) Decode(ctx context.Context, req *pb.DecodeRequest) (*pb.DecodeResponse, error) {
	log.Printf("Decode request: payloads=%d", len(req.Payloads))

	// Convert proto bytes to Temporal payloads
	payloads := make([]*common.Payload, len(req.Payloads))
	for i, data := range req.Payloads {
		payload := &common.Payload{}
		if err := proto.Unmarshal(data, payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload %d: %w", i, err)
		}
		payloads[i] = payload
	}

	// Decode payloads
	decoded, err := s.codec.Decode(ctx, payloads)
	if err != nil {
		log.Printf("Decode error: %v", err)
		return nil, fmt.Errorf("failed to decode payloads: %w", err)
	}

	// Convert back to proto bytes
	decodedBytes := make([][]byte, len(decoded))
	for i, payload := range decoded {
		data, err := proto.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal decoded payload %d: %w", i, err)
		}
		decodedBytes[i] = data
	}

	log.Printf("Decode response: %d payloads", len(decodedBytes))

	return &pb.DecodeResponse{
		DecodedPayloads: decodedBytes,
	}, nil
}
