package cache

import (
	"context"
	"time"
)

// CacheService defines the interface for a distributed cache system
type CacheService interface {
	// Get retrieves a value from the cache.
	// The implementation should unmarshal the data into the 'dest' pointer.
	Get(ctx context.Context, key string, dest interface{}) error

	// Set stores a value in the cache with a TTL.
	// The implementation should marshal the value.
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Delete removes a value from the cache.
	Delete(ctx context.Context, key string) error
}
