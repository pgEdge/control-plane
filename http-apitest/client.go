// Package httpapitest provides a raw HTTP API testing framework for Control Plane.
// It makes direct HTTP requests to the API endpoints (like curl) rather than
// using the Go client wrapper.
package httpapitest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Client is a raw HTTP client for testing the Control Plane API.
// It makes direct HTTP requests without using the generated Go client.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	t          *testing.T
}

// NewClient creates a new API test client.
func NewClient(t *testing.T, baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		t: t,
	}
}

// Request represents an HTTP request configuration.
type Request struct {
	Method      string
	Path        string
	Body        interface{}
	QueryParams map[string]string
	Headers     map[string]string
}

// Response wraps the HTTP response with helper methods.
type Response struct {
	*http.Response
	Body []byte
	t    *testing.T
}

// Do executes an HTTP request and returns the response.
func (c *Client) Do(ctx context.Context, req Request) *Response {
	// Build URL
	fullURL := c.BaseURL + req.Path
	if len(req.QueryParams) > 0 {
		params := url.Values{}
		for k, v := range req.QueryParams {
			params.Add(k, v)
		}
		fullURL += "?" + params.Encode()
	}

	// Marshal body
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		require.NoError(c.t, err, "failed to marshal request body")
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, fullURL, bodyReader)
	require.NoError(c.t, err, "failed to create HTTP request")

	// Set default headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Add custom headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	c.t.Logf("→ %s %s", req.Method, fullURL)
	httpResp, err := c.HTTPClient.Do(httpReq)
	require.NoError(c.t, err, "HTTP request failed")

	// Read response body
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	require.NoError(c.t, err, "failed to read response body")

	c.t.Logf("← %d %s (%d bytes)", httpResp.StatusCode, httpResp.Status, len(respBody))

	return &Response{
		Response: httpResp,
		Body:     respBody,
		t:        c.t,
	}
}

// UnmarshalJSON unmarshals the response body into the provided interface.
func (r *Response) UnmarshalJSON(v interface{}) {
	err := json.Unmarshal(r.Body, v)
	require.NoError(r.t, err, "failed to unmarshal JSON response: %s", string(r.Body))
}

// AssertStatusCode asserts that the response has the expected status code.
func (r *Response) AssertStatusCode(expected int) *Response {
	require.Equal(r.t, expected, r.StatusCode,
		"unexpected status code\nResponse body: %s", string(r.Body))
	return r
}

// AssertSuccess asserts that the response is in the 2xx range.
func (r *Response) AssertSuccess() *Response {
	require.True(r.t, r.StatusCode >= 200 && r.StatusCode < 300,
		"expected success status code, got %d\nResponse body: %s",
		r.StatusCode, string(r.Body))
	return r
}

// AssertError asserts that the response is in the 4xx or 5xx range.
func (r *Response) AssertError() *Response {
	require.True(r.t, r.StatusCode >= 400,
		"expected error status code, got %d", r.StatusCode)
	return r
}

// AssertJSONContains asserts that the response contains the expected JSON field/value.
func (r *Response) AssertJSONContains(key string, expectedValue interface{}) *Response {
	var data map[string]interface{}
	r.UnmarshalJSON(&data)

	actualValue, ok := data[key]
	require.True(r.t, ok, "key %q not found in response", key)
	require.Equal(r.t, expectedValue, actualValue,
		"unexpected value for key %q", key)
	return r
}

// GET performs a GET request.
func (c *Client) GET(ctx context.Context, path string, queryParams ...map[string]string) *Response {
	var params map[string]string
	if len(queryParams) > 0 {
		params = queryParams[0]
	}
	return c.Do(ctx, Request{
		Method:      http.MethodGet,
		Path:        path,
		QueryParams: params,
	})
}

// POST performs a POST request.
func (c *Client) POST(ctx context.Context, path string, body interface{}) *Response {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
}

// PUT performs a PUT request.
func (c *Client) PUT(ctx context.Context, path string, body interface{}) *Response {
	return c.Do(ctx, Request{
		Method: http.MethodPut,
		Path:   path,
		Body:   body,
	})
}

// DELETE performs a DELETE request.
func (c *Client) DELETE(ctx context.Context, path string) *Response {
	return c.Do(ctx, Request{
		Method: http.MethodDelete,
		Path:   path,
	})
}

// WaitForTask polls a task until it completes or times out.
func (c *Client) WaitForTask(ctx context.Context, databaseID, taskID string, timeout time.Duration) *Response {
	c.t.Logf("Waiting for task %s to complete (timeout: %s)", taskID, timeout)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("context canceled while waiting for task")
		case <-ticker.C:
			if time.Now().After(deadline) {
				c.t.Fatalf("timeout waiting for task %s", taskID)
			}

			resp := c.GET(ctx, fmt.Sprintf("/v1/databases/%s/tasks/%s", databaseID, taskID))
			resp.AssertSuccess()

			var task struct {
				Status string `json:"status"`
			}
			resp.UnmarshalJSON(&task)

			c.t.Logf("Task %s status: %s", taskID, task.Status)

			switch task.Status {
			case "completed":
				return resp
			case "failed", "canceled":
				c.t.Fatalf("task %s ended with status: %s", taskID, task.Status)
			}
		}
	}
}

// WaitForDatabaseStatus polls a database until it reaches the expected status or times out.
func (c *Client) WaitForDatabaseStatus(ctx context.Context, databaseID, expectedStatus string, timeout time.Duration) *Response {
	c.t.Logf("Waiting for database %s to reach status %s (timeout: %s)", databaseID, expectedStatus, timeout)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("context canceled while waiting for database")
		case <-ticker.C:
			if time.Now().After(deadline) {
				c.t.Fatalf("timeout waiting for database %s to reach status %s", databaseID, expectedStatus)
			}

			resp := c.GET(ctx, fmt.Sprintf("/v1/databases/%s", databaseID))
			resp.AssertSuccess()

			var db struct {
				Status string `json:"status"`
			}
			resp.UnmarshalJSON(&db)

			c.t.Logf("Database %s status: %s", databaseID, db.Status)

			if db.Status == expectedStatus {
				return resp
			}
		}
	}
}
