package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/vsemashko/large-files-temporal/codec-server/internal/codec"
	"github.com/vsemashko/large-files-temporal/codec-server/internal/metrics"
	"go.temporal.io/api/common/v1"
	"google.golang.org/protobuf/proto"
)

// Server implements the HTTP server for the codec
type Server struct {
	codec *codec.Codec
}

// NewServer creates a new HTTP server
func NewServer(c *codec.Codec) *Server {
	return &Server{
		codec: c,
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

// HandleHealth handles health check endpoint
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// HandleMetrics handles metrics endpoint
func (s *Server) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics.Handler()(w, r)
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
