package codec

import (
	"context"
	"fmt"
	"log"
	"sync"

	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	pb "github.com/vsemashko/large-files-temporal/samples/go-worker/pkg/proto"
)

// WorkflowContext holds workflow execution information
type WorkflowContext struct {
	Namespace  string
	WorkflowID string
	RunID      string
}

// ContextKey is used for storing workflow context
type contextKey int

const (
	workflowContextKey contextKey = iota
)

// RemotePayloadCodec implements a remote codec that communicates with the codec server via gRPC
type RemotePayloadCodec struct {
	client      pb.PayloadCodecClient
	conn        *grpc.ClientConn
	contextLock sync.RWMutex
	ctxMap      map[uint64]*WorkflowContext // goroutine ID -> context
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
		ctxMap: make(map[uint64]*WorkflowContext),
	}, nil
}

// Encode encodes payloads by calling the remote codec server
func (r *RemotePayloadCodec) Encode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	if len(payloads) == 0 {
		return payloads, nil
	}

	// Try to extract workflow context from payload metadata
	// This is a fallback approach when interceptors aren't available
	wfCtx := r.extractWorkflowContext(payloads)

	// Serialize payloads to bytes
	payloadBytes := make([][]byte, len(payloads))
	for i, p := range payloads {
		data, err := proto.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal payload %d: %w", i, err)
		}
		payloadBytes[i] = data
	}

	// Call remote codec server with extracted context
	resp, err := r.client.Encode(context.Background(), &pb.EncodeRequest{
		Payloads:   payloadBytes,
		Namespace:  wfCtx.Namespace,
		WorkflowId: wfCtx.WorkflowID,
		RunId:      wfCtx.RunID,
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

	if wfCtx.WorkflowID != "unknown" {
		log.Printf("Encoded %d payloads for workflow %s/%s", len(encoded), wfCtx.WorkflowID, wfCtx.RunID)
	} else {
		log.Printf("Encoded %d payloads (workflow context unknown)", len(encoded))
	}
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

// extractWorkflowContext attempts to extract workflow context from payload metadata
func (r *RemotePayloadCodec) extractWorkflowContext(payloads []*commonpb.Payload) *WorkflowContext {
	// Default context
	ctx := &WorkflowContext{
		Namespace:  "default",
		WorkflowID: "unknown",
		RunID:      "unknown",
	}

	// Try to find temporal metadata in payloads
	for _, payload := range payloads {
		if payload == nil || payload.Metadata == nil {
			continue
		}

		// Check for Temporal's internal workflow info metadata
		// Temporal stores this in specific metadata keys
		if workflowID, ok := payload.Metadata["temporal-workflow-id"]; ok {
			ctx.WorkflowID = string(workflowID)
		}
		if runID, ok := payload.Metadata["temporal-run-id"]; ok {
			ctx.RunID = string(runID)
		}
		if namespace, ok := payload.Metadata["temporal-namespace"]; ok {
			ctx.Namespace = string(namespace)
		}

		// If we found workflow info, we're done
		if ctx.WorkflowID != "unknown" {
			break
		}
	}

	return ctx
}

// Close closes the connection to the codec server
func (r *RemotePayloadCodec) Close() error {
	return r.conn.Close()
}

// ContextAwareCodec wraps the remote codec with workflow context injection
type ContextAwareCodec struct {
	codec *RemotePayloadCodec
}

// NewContextAwareCodec creates a context-aware codec wrapper
func NewContextAwareCodec(remoteCodec *RemotePayloadCodec) converter.PayloadCodec {
	return &ContextAwareCodec{
		codec: remoteCodec,
	}
}

// Encode implements PayloadCodec interface
func (c *ContextAwareCodec) Encode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	return c.codec.Encode(payloads)
}

// Decode implements PayloadCodec interface
func (c *ContextAwareCodec) Decode(payloads []*commonpb.Payload) ([]*commonpb.Payload, error) {
	return c.codec.Decode(payloads)
}

// WorkflowContextInjector is a helper that can inject workflow context into payloads
// This should be used in workflow code to add context metadata
type WorkflowContextInjector struct{}

// InjectContext adds workflow context to payload metadata
func (w *WorkflowContextInjector) InjectContext(ctx workflow.Context, payload *commonpb.Payload) *commonpb.Payload {
	if payload == nil {
		return payload
	}

	info := workflow.GetInfo(ctx)

	if payload.Metadata == nil {
		payload.Metadata = make(map[string][]byte)
	}

	payload.Metadata["temporal-workflow-id"] = []byte(info.WorkflowExecution.ID)
	payload.Metadata["temporal-run-id"] = []byte(info.WorkflowExecution.RunID)
	payload.Metadata["temporal-namespace"] = []byte(info.Namespace)

	return payload
}
