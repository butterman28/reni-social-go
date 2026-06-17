package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	// "time"
	"dogo/internal/types"
)

// DoRequest executes an HTTP request and returns response body or error
func DoRequest(options types.RequestOptions, result interface{}) error {
	// Marshal body if present
	var bodyBytes []byte
	if options.Body != nil {
		var err error
		bodyBytes, err = json.Marshal(options.Body)
		if err != nil {
			return fmt.Errorf("failed to marshal body: %v", err)
		}
	}

	// Create HTTP request
	req, err := http.NewRequest(options.Method, options.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	for k, v := range options.Headers {
		req.Header.Set(k, v)
	}

	// Set timeout
	client := &http.Client{
		Timeout: options.Timeout,
	}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Optional: handle HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	// Decode JSON if result is provided
	if result != nil {
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("failed to decode response JSON: %v", err)
		}
	}

	return nil
}
