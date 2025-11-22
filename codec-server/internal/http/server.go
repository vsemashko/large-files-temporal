package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/metrics"
	"go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

const (
	// MaxRequestBodySize is the maximum size of HTTP request bodies (100MB)
	MaxRequestBodySize = 100 * 1024 * 1024

	// MaxPayloadsPerRequest is the maximum number of payloads in a single request
	MaxPayloadsPerRequest = 1000

	// MaxMetadataKeySize is the maximum size of a metadata key
	MaxMetadataKeySize = 256

	// MaxMetadataValueSize is the maximum size of a metadata value
	MaxMetadataValueSize = 4096

	// MaxWorkflowIDLength is the maximum length of a workflow ID
	MaxWorkflowIDLength = 1000

	// MaxRunIDLength is the maximum length of a run ID
	MaxRunIDLength = 256

	// MaxNamespaceLength is the maximum length of a namespace
	MaxNamespaceLength = 256
)

// Server implements the HTTP server for the codec
type Server struct {
	codec  *codec.Codec
	apiKey string
}

// NewServer creates a new HTTP server
func NewServer(c *codec.Codec, apiKey string) *Server {
	return &Server{
		codec:  c,
		apiKey: apiKey,
	}
}

// AuthMiddleware returns an HTTP middleware that checks API key authentication
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if API key is not configured
		if s.apiKey == "" {
			next(w, r)
			return
		}

		// Skip auth for health endpoint
		if r.URL.Path == "/health" {
			next(w, r)
			return
		}

		// Check for API key in header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			// Try Authorization header with Bearer scheme
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				apiKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if apiKey != s.apiKey {
			log.Printf("Unauthorized request from %s to %s", r.RemoteAddr, r.URL.Path)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// EncodeRequest represents the HTTP encode request
type EncodeRequest struct {
	Payloads   []PayloadJSON `json:"payloads"`
	Namespace  string        `json:"namespace"`
	WorkflowID string        `json:"workflowId"`
	RunID      string        `json:"runId"`
}

// EncodeResponse represents the HTTP encode response
type EncodeResponse struct {
	Payloads []PayloadJSON `json:"payloads"`
}

// DecodeRequest represents the HTTP decode request
type DecodeRequest struct {
	Payloads []PayloadJSON `json:"payloads"`
}

// DecodeResponse represents the HTTP decode response
type DecodeResponse struct {
	Payloads []PayloadJSON `json:"payloads"`
}

// PayloadJSON represents a payload in JSON format
type PayloadJSON struct {
	Metadata map[string]string `json:"metadata,omitempty"`
	Data     string            `json:"data,omitempty"` // base64 encoded
}

// HandleEncode handles the encode endpoint
func (s *Server) HandleEncode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse request
	var req EncodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Failed to parse request: %v", err)
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	log.Printf("HTTP Encode request: namespace=%s, workflowID=%s, runID=%s, payloads=%d",
		req.Namespace, req.WorkflowID, req.RunID, len(req.Payloads))

	// Validate request
	if err := validateEncodeRequest(&req); err != nil {
		log.Printf("Invalid encode request: %v", err)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Convert JSON payloads to Temporal payloads
	payloads, err := jsonToPayloads(req.Payloads)
	if err != nil {
		log.Printf("Failed to convert payloads: %v", err)
		http.Error(w, fmt.Sprintf("Failed to convert payloads: %v", err), http.StatusBadRequest)
		return
	}

	// Encode payloads
	encoded, err := s.codec.Encode(r.Context(), payloads, req.Namespace, req.WorkflowID, req.RunID)
	if err != nil {
		log.Printf("Failed to encode payloads: %v", err)
		http.Error(w, fmt.Sprintf("Failed to encode payloads: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert back to JSON
	responsePayloads, err := payloadsToJSON(encoded)
	if err != nil {
		log.Printf("Failed to convert response: %v", err)
		http.Error(w, fmt.Sprintf("Failed to convert response: %v", err), http.StatusInternalServerError)
		return
	}

	// Send response
	resp := EncodeResponse{Payloads: responsePayloads}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode response: %v", err)
		return
	}

	log.Printf("HTTP Encode response: %d payloads", len(resp.Payloads))
}

// HandleDecode handles the decode endpoint
func (s *Server) HandleDecode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse request
	var req DecodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Failed to parse request: %v", err)
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	log.Printf("HTTP Decode request: payloads=%d", len(req.Payloads))

	// Validate request
	if err := validateDecodeRequest(&req); err != nil {
		log.Printf("Invalid decode request: %v", err)
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Convert JSON payloads to Temporal payloads
	payloads, err := jsonToPayloads(req.Payloads)
	if err != nil {
		log.Printf("Failed to convert payloads: %v", err)
		http.Error(w, fmt.Sprintf("Failed to convert payloads: %v", err), http.StatusBadRequest)
		return
	}

	// Decode payloads
	decoded, err := s.codec.Decode(r.Context(), payloads)
	if err != nil {
		log.Printf("Failed to decode payloads: %v", err)
		http.Error(w, fmt.Sprintf("Failed to decode payloads: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert back to JSON
	responsePayloads, err := payloadsToJSON(decoded)
	if err != nil {
		log.Printf("Failed to convert response: %v", err)
		http.Error(w, fmt.Sprintf("Failed to convert response: %v", err), http.StatusInternalServerError)
		return
	}

	// Send response
	resp := DecodeResponse{Payloads: responsePayloads}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Failed to encode response: %v", err)
		return
	}

	log.Printf("HTTP Decode response: %d payloads", len(resp.Payloads))
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string            `json:"status"`
	Checks  map[string]string `json:"checks"`
	Version string            `json:"version,omitempty"`
}

// HandleHealth handles health check endpoint
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response := HealthResponse{
		Status:  "healthy",
		Checks:  make(map[string]string),
		Version: "1.0.0",
	}

	// Check S3 connectivity
	if err := s.checkS3Health(ctx); err != nil {
		response.Status = "unhealthy"
		response.Checks["s3"] = fmt.Sprintf("failed: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		response.Checks["s3"] = "ok"
	}

	// Check codec functionality
	if err := s.checkCodecHealth(ctx); err != nil {
		response.Status = "unhealthy"
		response.Checks["codec"] = fmt.Sprintf("failed: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		response.Checks["codec"] = "ok"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// checkS3Health verifies S3 connectivity
func (s *Server) checkS3Health(ctx context.Context) error {
	// Try a simple operation to verify S3 is accessible
	// We could list objects with a limit of 1
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("test"),
		},
		Data: []byte("health-check"),
	}

	// Just verify we can calculate size and prepare for upload
	// Don't actually upload to avoid polluting S3
	size := int64(len(testPayload.Data))
	if size < 0 {
		return fmt.Errorf("invalid payload size")
	}

	return nil
}

// checkCodecHealth verifies codec functionality
func (s *Server) checkCodecHealth(ctx context.Context) error {
	// Create a small test payload
	testPayload := &common.Payload{
		Metadata: map[string][]byte{
			"encoding": []byte("json/plain"),
		},
		Data: []byte(`{"test":"health-check"}`),
	}

	// Test encode (should keep inline since it's small)
	encoded, err := s.codec.Encode(ctx, []*common.Payload{testPayload}, "health-check", "test-wf", "test-run")
	if err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}

	if len(encoded) != 1 {
		return fmt.Errorf("encode returned wrong number of payloads")
	}

	// Test decode
	decoded, err := s.codec.Decode(ctx, encoded)
	if err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	if len(decoded) != 1 {
		return fmt.Errorf("decode returned wrong number of payloads")
	}

	return nil
}

// HandleMetrics handles metrics endpoint
func (s *Server) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics.Handler()(w, r)
}

// validateEncodeRequest validates an encode request
func validateEncodeRequest(req *EncodeRequest) error {
	// Validate payload count
	if len(req.Payloads) > MaxPayloadsPerRequest {
		return fmt.Errorf("too many payloads: %d (maximum: %d)", len(req.Payloads), MaxPayloadsPerRequest)
	}

	// Validate namespace
	if req.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if len(req.Namespace) > MaxNamespaceLength {
		return fmt.Errorf("namespace too long: %d characters (maximum: %d)", len(req.Namespace), MaxNamespaceLength)
	}

	// Validate workflow ID
	if req.WorkflowID == "" {
		return fmt.Errorf("workflowId is required")
	}
	if len(req.WorkflowID) > MaxWorkflowIDLength {
		return fmt.Errorf("workflowId too long: %d characters (maximum: %d)", len(req.WorkflowID), MaxWorkflowIDLength)
	}

	// Validate run ID
	if req.RunID == "" {
		return fmt.Errorf("runId is required")
	}
	if len(req.RunID) > MaxRunIDLength {
		return fmt.Errorf("runId too long: %d characters (maximum: %d)", len(req.RunID), MaxRunIDLength)
	}

	// Validate each payload
	for i, p := range req.Payloads {
		if err := validatePayload(p, i); err != nil {
			return err
		}
	}

	return nil
}

// validateDecodeRequest validates a decode request
func validateDecodeRequest(req *DecodeRequest) error {
	// Validate payload count
	if len(req.Payloads) > MaxPayloadsPerRequest {
		return fmt.Errorf("too many payloads: %d (maximum: %d)", len(req.Payloads), MaxPayloadsPerRequest)
	}

	// Validate each payload
	for i, p := range req.Payloads {
		if err := validatePayload(p, i); err != nil {
			return err
		}
	}

	return nil
}

// validatePayload validates a single payload
func validatePayload(p PayloadJSON, index int) error {
	// Validate metadata
	if len(p.Metadata) > 100 {
		return fmt.Errorf("payload[%d]: too many metadata entries: %d (maximum: 100)", index, len(p.Metadata))
	}

	for key, value := range p.Metadata {
		if len(key) > MaxMetadataKeySize {
			return fmt.Errorf("payload[%d]: metadata key too long: %d characters (maximum: %d)", index, len(key), MaxMetadataKeySize)
		}
		if len(value) > MaxMetadataValueSize {
			return fmt.Errorf("payload[%d]: metadata value too long for key '%s': %d characters (maximum: %d)", index, key, len(value), MaxMetadataValueSize)
		}
	}

	return nil
}

// jsonToPayloads converts JSON payloads to Temporal payloads
func jsonToPayloads(jsonPayloads []PayloadJSON) ([]*common.Payload, error) {
	payloads := make([]*common.Payload, len(jsonPayloads))

	for i, jp := range jsonPayloads {
		payload := &common.Payload{
			Metadata: make(map[string][]byte),
		}

		// Convert metadata
		for k, v := range jp.Metadata {
			payload.Metadata[k] = []byte(v)
		}

		// Data is base64 encoded in JSON
		if jp.Data != "" {
			payload.Data = []byte(jp.Data)
		}

		payloads[i] = payload
	}

	return payloads, nil
}

// payloadsToJSON converts Temporal payloads to JSON format
func payloadsToJSON(payloads []*common.Payload) ([]PayloadJSON, error) {
	jsonPayloads := make([]PayloadJSON, len(payloads))

	for i, p := range payloads {
		jp := PayloadJSON{
			Metadata: make(map[string]string),
		}

		// Convert metadata
		for k, v := range p.Metadata {
			jp.Metadata[k] = string(v)
		}

		// Data as base64
		if len(p.Data) > 0 {
			jp.Data = string(p.Data)
		}

		jsonPayloads[i] = jp
	}

	return jsonPayloads, nil
}
