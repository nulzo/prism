package httpclient

import (
	"net"
	"net/http"
	"time"
)

const (
	defaultDialTimeout           = 30 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 2 * time.Minute
	defaultStreamingHeaderTimeout = 10 * time.Minute
	defaultExpectContinueTimeout = 1 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultMaxIdleConns          = 500
	defaultMaxIdleConnsPerHost   = 500
	defaultMaxConnsPerHost       = 500
)

func effectiveResponseHeaderTimeout(requestTimeout, fallback time.Duration) time.Duration {
	if requestTimeout > 0 {
		return requestTimeout
	}
	return fallback
}

func newTransport(headerTimeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		MaxConnsPerHost:       defaultMaxConnsPerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: defaultExpectContinueTimeout,
		ResponseHeaderTimeout: headerTimeout,
	}
}

// NewRequestClient creates a client for regular JSON requests. The timeout
// caps the full request/response lifetime, which is fine for non-streaming
// calls like chat completions, model discovery, and health checks.
func NewRequestClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: newTransport(effectiveResponseHeaderTimeout(
			timeout,
			defaultResponseHeaderTimeout,
		)),
	}
}

// NewStreamingClient creates a client for SSE / long-lived streaming calls.
// Intentionally leaves http.Client.Timeout unset so the response body can stay
// open indefinitely once headers have arrived. responseHeaderTimeout bounds
// time-to-first-byte; pass the provider request timeout when available.
func NewStreamingClient(headerTimeout time.Duration) *http.Client {
	return &http.Client{
		Transport: newTransport(effectiveResponseHeaderTimeout(
			headerTimeout,
			defaultStreamingHeaderTimeout,
		)),
	}
}
