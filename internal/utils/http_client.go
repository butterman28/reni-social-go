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
// DoRequest godoc
// @Summary DoRequest
// @Description Auto-generated thorough documentation for DoRequest
// @Tags utils
// @Produce json
// @Param options body string true "options"
// @Param result body string true "result"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
