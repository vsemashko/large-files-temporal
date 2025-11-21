package storage

// S3Reference represents a reference to a payload stored in S3
type S3Reference struct {
	Type     string `json:"type"`
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Hash     string `json:"hash"`
}

const (
	// EncodingS3Reference is the encoding type for S3 references
	EncodingS3Reference = "s3-reference"

	// MetadataEncodingKey is the key for encoding metadata
	MetadataEncodingKey = "encoding"
)
