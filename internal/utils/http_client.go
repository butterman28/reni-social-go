package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RequestOptions configures an outgoing HTTP request.
type RequestOptions struct {
	Method  string
	URL     string
	Body    any
	Headers map[string]string
	Timeout time.Duration
}

// DoRequest executes an HTTP request, marshals the body, and decodes the JSON response.
func DoRequest(options RequestOptions, result any) error {
	var bodyBytes []byte
	if options.Body != nil {
		var err error
		bodyBytes, err = json.Marshal(options.Body)
		if err != nil {
			return fmt.Errorf("marshal body: %v", err)
		}
	}

	req, err := http.NewRequest(options.Method, options.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range options.Headers {
		req.Header.Set(k, v)
	}

	timeout := options.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	client := &http.Client{Timeout: timeout}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	if result != nil {
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("decode response JSON: %v", err)
		}
	}

	return nil
}
