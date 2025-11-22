package storage

import "errors"

// Common storage errors
var (
	// ErrNotFound is returned when an object is not found in storage
	ErrNotFound = errors.New("object not found in storage")

	// ErrAlreadyExists is returned when an object already exists
	ErrAlreadyExists = errors.New("object already exists")

	// ErrInvalidKey is returned when a storage key is invalid
	ErrInvalidKey = errors.New("invalid storage key")

	// ErrPermissionDenied is returned when access to an object is denied
	ErrPermissionDenied = errors.New("permission denied")

	// ErrQuotaExceeded is returned when storage quota is exceeded
	ErrQuotaExceeded = errors.New("storage quota exceeded")
)
