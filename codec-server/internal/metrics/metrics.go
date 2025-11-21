package metrics

import (
	"net/http"
	"sync"
	"time"
)

// Metrics tracks codec server metrics
type Metrics struct {
	mu sync.RWMutex

	// Encoding metrics
	TotalEncodeRequests   int64
	TotalDecodeRequests   int64
	TotalPayloadsEncoded  int64
	TotalPayloadsDecoded  int64
	TotalS3Uploads        int64
	TotalS3Downloads      int64
	TotalEncodeErrors     int64
	TotalDecodeErrors     int64
	TotalS3UploadErrors   int64
	TotalS3DownloadErrors int64

	// Size metrics
	TotalBytesUploaded   int64
	TotalBytesDownloaded int64
	LargestPayloadSize   int64
	SmallestPayloadSize  int64

	// Latency metrics
	AvgEncodeLatencyMs  int64
	AvgDecodeLatencyMs  int64
	AvgS3UploadLatencyMs int64
	AvgS3DownloadLatencyMs int64

	// Histogram data (for calculating averages)
	encodeLatencies []time.Duration
	decodeLatencies []time.Duration
	s3UploadLatencies []time.Duration
	s3DownloadLatencies []time.Duration
}

var (
	globalMetrics = &Metrics{
		SmallestPayloadSize: -1,
	}
)

// GetMetrics returns the global metrics instance
func GetMetrics() *Metrics {
	return globalMetrics
}

// RecordEncode records an encode operation
func (m *Metrics) RecordEncode(payloadCount int, duration time.Duration, hasError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalEncodeRequests++
	m.TotalPayloadsEncoded += int64(payloadCount)
	m.encodeLatencies = append(m.encodeLatencies, duration)

	if hasError {
		m.TotalEncodeErrors++
	}

	m.updateAvgLatency()
}

// RecordDecode records a decode operation
func (m *Metrics) RecordDecode(payloadCount int, duration time.Duration, hasError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalDecodeRequests++
	m.TotalPayloadsDecoded += int64(payloadCount)
	m.decodeLatencies = append(m.decodeLatencies, duration)

	if hasError {
		m.TotalDecodeErrors++
	}

	m.updateAvgLatency()
}

// RecordS3Upload records an S3 upload operation
func (m *Metrics) RecordS3Upload(size int64, duration time.Duration, hasError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalS3Uploads++
	m.TotalBytesUploaded += size
	m.s3UploadLatencies = append(m.s3UploadLatencies, duration)

	if hasError {
		m.TotalS3UploadErrors++
	}

	// Update size metrics
	if m.LargestPayloadSize < size {
		m.LargestPayloadSize = size
	}
	if m.SmallestPayloadSize < 0 || m.SmallestPayloadSize > size {
		m.SmallestPayloadSize = size
	}

	m.updateAvgLatency()
}

// RecordS3Download records an S3 download operation
func (m *Metrics) RecordS3Download(size int64, duration time.Duration, hasError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalS3Downloads++
	m.TotalBytesDownloaded += size
	m.s3DownloadLatencies = append(m.s3DownloadLatencies, duration)

	if hasError {
		m.TotalS3DownloadErrors++
	}

	m.updateAvgLatency()
}

// updateAvgLatency updates average latencies (must be called with lock held)
func (m *Metrics) updateAvgLatency() {
	m.AvgEncodeLatencyMs = calculateAvg(m.encodeLatencies)
	m.AvgDecodeLatencyMs = calculateAvg(m.decodeLatencies)
	m.AvgS3UploadLatencyMs = calculateAvg(m.s3UploadLatencies)
	m.AvgS3DownloadLatencyMs = calculateAvg(m.s3DownloadLatencies)

	// Keep only last 1000 samples to avoid memory growth
	if len(m.encodeLatencies) > 1000 {
		m.encodeLatencies = m.encodeLatencies[len(m.encodeLatencies)-1000:]
	}
	if len(m.decodeLatencies) > 1000 {
		m.decodeLatencies = m.decodeLatencies[len(m.decodeLatencies)-1000:]
	}
	if len(m.s3UploadLatencies) > 1000 {
		m.s3UploadLatencies = m.s3UploadLatencies[len(m.s3UploadLatencies)-1000:]
	}
	if len(m.s3DownloadLatencies) > 1000 {
		m.s3DownloadLatencies = m.s3DownloadLatencies[len(m.s3DownloadLatencies)-1000:]
	}
}

// calculateAvg calculates average duration in milliseconds
func calculateAvg(durations []time.Duration) int64 {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}

	return total.Milliseconds() / int64(len(durations))
}

// GetSnapshot returns a read-only snapshot of the metrics
func (m *Metrics) GetSnapshot() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		TotalEncodeRequests:    m.TotalEncodeRequests,
		TotalDecodeRequests:    m.TotalDecodeRequests,
		TotalPayloadsEncoded:   m.TotalPayloadsEncoded,
		TotalPayloadsDecoded:   m.TotalPayloadsDecoded,
		TotalS3Uploads:         m.TotalS3Uploads,
		TotalS3Downloads:       m.TotalS3Downloads,
		TotalEncodeErrors:      m.TotalEncodeErrors,
		TotalDecodeErrors:      m.TotalDecodeErrors,
		TotalS3UploadErrors:    m.TotalS3UploadErrors,
		TotalS3DownloadErrors:  m.TotalS3DownloadErrors,
		TotalBytesUploaded:     m.TotalBytesUploaded,
		TotalBytesDownloaded:   m.TotalBytesDownloaded,
		LargestPayloadSize:     m.LargestPayloadSize,
		SmallestPayloadSize:    m.SmallestPayloadSize,
		AvgEncodeLatencyMs:     m.AvgEncodeLatencyMs,
		AvgDecodeLatencyMs:     m.AvgDecodeLatencyMs,
		AvgS3UploadLatencyMs:   m.AvgS3UploadLatencyMs,
		AvgS3DownloadLatencyMs: m.AvgS3DownloadLatencyMs,
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	TotalEncodeRequests    int64 `json:"total_encode_requests"`
	TotalDecodeRequests    int64 `json:"total_decode_requests"`
	TotalPayloadsEncoded   int64 `json:"total_payloads_encoded"`
	TotalPayloadsDecoded   int64 `json:"total_payloads_decoded"`
	TotalS3Uploads         int64 `json:"total_s3_uploads"`
	TotalS3Downloads       int64 `json:"total_s3_downloads"`
	TotalEncodeErrors      int64 `json:"total_encode_errors"`
	TotalDecodeErrors      int64 `json:"total_decode_errors"`
	TotalS3UploadErrors    int64 `json:"total_s3_upload_errors"`
	TotalS3DownloadErrors  int64 `json:"total_s3_download_errors"`
	TotalBytesUploaded     int64 `json:"total_bytes_uploaded"`
	TotalBytesDownloaded   int64 `json:"total_bytes_downloaded"`
	LargestPayloadSize     int64 `json:"largest_payload_size"`
	SmallestPayloadSize    int64 `json:"smallest_payload_size"`
	AvgEncodeLatencyMs     int64 `json:"avg_encode_latency_ms"`
	AvgDecodeLatencyMs     int64 `json:"avg_decode_latency_ms"`
	AvgS3UploadLatencyMs   int64 `json:"avg_s3_upload_latency_ms"`
	AvgS3DownloadLatencyMs int64 `json:"avg_s3_download_latency_ms"`
}

// Handler returns an HTTP handler for the metrics endpoint
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := GetMetrics()
		snapshot := m.GetSnapshot()

		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		// Prometheus-style metrics
		writeMetric(w, "codec_encode_requests_total", snapshot.TotalEncodeRequests)
		writeMetric(w, "codec_decode_requests_total", snapshot.TotalDecodeRequests)
		writeMetric(w, "codec_payloads_encoded_total", snapshot.TotalPayloadsEncoded)
		writeMetric(w, "codec_payloads_decoded_total", snapshot.TotalPayloadsDecoded)
		writeMetric(w, "codec_s3_uploads_total", snapshot.TotalS3Uploads)
		writeMetric(w, "codec_s3_downloads_total", snapshot.TotalS3Downloads)
		writeMetric(w, "codec_encode_errors_total", snapshot.TotalEncodeErrors)
		writeMetric(w, "codec_decode_errors_total", snapshot.TotalDecodeErrors)
		writeMetric(w, "codec_s3_upload_errors_total", snapshot.TotalS3UploadErrors)
		writeMetric(w, "codec_s3_download_errors_total", snapshot.TotalS3DownloadErrors)
		writeMetric(w, "codec_bytes_uploaded_total", snapshot.TotalBytesUploaded)
		writeMetric(w, "codec_bytes_downloaded_total", snapshot.TotalBytesDownloaded)
		writeMetric(w, "codec_largest_payload_bytes", snapshot.LargestPayloadSize)
		if snapshot.SmallestPayloadSize >= 0 {
			writeMetric(w, "codec_smallest_payload_bytes", snapshot.SmallestPayloadSize)
		}
		writeMetric(w, "codec_avg_encode_latency_ms", snapshot.AvgEncodeLatencyMs)
		writeMetric(w, "codec_avg_decode_latency_ms", snapshot.AvgDecodeLatencyMs)
		writeMetric(w, "codec_avg_s3_upload_latency_ms", snapshot.AvgS3UploadLatencyMs)
		writeMetric(w, "codec_avg_s3_download_latency_ms", snapshot.AvgS3DownloadLatencyMs)
	}
}

func writeMetric(w http.ResponseWriter, name string, value int64) {
	w.Write([]byte(name + " " + formatInt64(value) + "\n"))
}

func formatInt64(v int64) string {
	// Simple integer to string conversion
	if v == 0 {
		return "0"
	}

	negative := v < 0
	if negative {
		v = -v
	}

	var result []byte
	for v > 0 {
		result = append([]byte{byte('0' + v%10)}, result...)
		v /= 10
	}

	if negative {
		result = append([]byte{'-'}, result...)
	}

	return string(result)
}
