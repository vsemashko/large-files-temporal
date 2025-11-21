package codec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/metrics"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/storage"
	"go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

// Codec handles encoding and decoding of Temporal payloads
type Codec struct {
	storage   storage.Storage
	threshold int64
	bucket    string
}

// NewCodec creates a new Codec instance
func NewCodec(s storage.Storage, threshold int64, bucket string) *Codec {
	return &Codec{
		storage:   s,
		threshold: threshold,
		bucket:    bucket,
	}
}

// Encode encodes payloads, storing large ones in S3
func (c *Codec) Encode(ctx context.Context, payloads []*common.Payload, namespace, workflowID, runID string) ([]*common.Payload, error) {
	start := time.Now()
	var err error
	defer func() {
		metrics.GetMetrics().RecordEncode(len(payloads), time.Since(start), err != nil)
	}()

	if len(payloads) == 0 {
		return payloads, nil
	}

	encoded := make([]*common.Payload, len(payloads))

	for i, payload := range payloads {
		if payload == nil {
			encoded[i] = nil
			continue
		}

		size := calculatePayloadSize(payload)

		if size < c.threshold {
			// Payload is small enough, keep it inline
			encoded[i] = payload
			log.Printf("Payload %d is %d bytes (threshold: %d), keeping inline", i, size, c.threshold)
			continue
		}

		// Payload is too large, store in S3
		log.Printf("Payload %d is %d bytes (threshold: %d), storing in S3", i, size, c.threshold)
		ref, storeErr := c.storePayload(ctx, payload, namespace, workflowID, runID)
		if storeErr != nil {
			err = storeErr
			return nil, fmt.Errorf("failed to store payload %d: %w", i, storeErr)
		}

		encoded[i] = ref
	}

	return encoded, nil
}

// Decode decodes payloads, retrieving large ones from S3
func (c *Codec) Decode(ctx context.Context, payloads []*common.Payload) ([]*common.Payload, error) {
	start := time.Now()
	var err error
	defer func() {
		metrics.GetMetrics().RecordDecode(len(payloads), time.Since(start), err != nil)
	}()

	if len(payloads) == 0 {
		return payloads, nil
	}

	decoded := make([]*common.Payload, len(payloads))

	for i, payload := range payloads {
		if payload == nil {
			decoded[i] = nil
			continue
		}

		if !isS3Reference(payload) {
			// Not an S3 reference, return as-is
			decoded[i] = payload
			continue
		}

		// S3 reference, retrieve from S3
		log.Printf("Payload %d is S3 reference, retrieving from S3", i)
		original, retrieveErr := c.retrievePayload(ctx, payload)
		if retrieveErr != nil {
			err = retrieveErr
			return nil, fmt.Errorf("failed to retrieve payload %d: %w", i, retrieveErr)
		}

		decoded[i] = original
	}

	return decoded, nil
}

// storePayload stores a payload in S3 and returns a reference payload
func (c *Codec) storePayload(ctx context.Context, payload *common.Payload, namespace, workflowID, runID string) (*common.Payload, error) {
	// Serialize payload
	data, err := proto.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Calculate hash
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Generate S3 key
	key := fmt.Sprintf("%s/%s/%s/%s", namespace, workflowID, runID, hashStr)

	// Get original encoding
	originalEncoding := ""
	if encodingBytes, ok := payload.Metadata[storage.MetadataEncodingKey]; ok {
		originalEncoding = string(encodingBytes)
	}

	// Upload to S3
	metadata := map[string]string{
		"workflow-id": workflowID,
		"run-id":      runID,
		"namespace":   namespace,
		"archived":    "false",
	}

	// Add timeout for S3 upload operation
	uploadCtx, uploadCancel := context.WithTimeout(ctx, 30*time.Second)
	defer uploadCancel()

	if err := c.storage.Upload(uploadCtx, key, data, metadata); err != nil {
		return nil, fmt.Errorf("failed to upload to S3: %w", err)
	}

	log.Printf("Stored payload in S3: %s (size: %d bytes)", key, len(data))

	// Create reference payload
	ref := storage.S3Reference{
		Type:     storage.EncodingS3Reference,
		Bucket:   c.bucket,
		Key:      key,
		Size:     int64(len(data)),
		Encoding: originalEncoding,
		Hash:     hashStr,
	}

	refData, err := json.Marshal(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reference: %w", err)
	}

	return &common.Payload{
		Metadata: map[string][]byte{
			storage.MetadataEncodingKey: []byte(storage.EncodingS3Reference),
		},
		Data: refData,
	}, nil
}

// retrievePayload retrieves a payload from S3 using a reference payload
func (c *Codec) retrievePayload(ctx context.Context, refPayload *common.Payload) (*common.Payload, error) {
	var ref storage.S3Reference
	if err := json.Unmarshal(refPayload.Data, &ref); err != nil {
		return nil, fmt.Errorf("failed to unmarshal reference: %w", err)
	}

	// Download from S3 with timeout
	downloadCtx, downloadCancel := context.WithTimeout(ctx, 30*time.Second)
	defer downloadCancel()

	data, err := c.storage.Download(downloadCtx, ref.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	log.Printf("Retrieved payload from S3: %s (size: %d bytes)", ref.Key, len(data))

	// Deserialize payload
	payload := &common.Payload{}
	if err := proto.Unmarshal(data, payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return payload, nil
}

// isS3Reference checks if a payload is an S3 reference
func isS3Reference(payload *common.Payload) bool {
	if payload == nil || payload.Metadata == nil {
		return false
	}

	encoding, ok := payload.Metadata[storage.MetadataEncodingKey]
	return ok && string(encoding) == storage.EncodingS3Reference
}

// calculatePayloadSize calculates the total size of a payload
func calculatePayloadSize(payload *common.Payload) int64 {
	if payload == nil {
		return 0
	}

	size := int64(len(payload.Data))

	for k, v := range payload.Metadata {
		size += int64(len(k) + len(v))
	}

	return size
}
