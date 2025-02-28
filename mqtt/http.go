package mqtt

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// HTTPServer is an HTTP server that communicates over MQTT. It works by reading
// and writing raw HTTP requests/responses in the MQTT message payloads.
type HTTPServer struct {
	endpoint Endpoint
	handler  http.Handler
}

type HTTPServerConfig struct {
	Topic          string
	Broker         BrokerConfig
	HandlerTimeout time.Duration
	Handler        http.Handler
	Logger         *zerolog.Logger
}

func NewHTTPServer(config HTTPServerConfig) *HTTPServer {
	endpoint := New(Config{
		Broker:         config.Broker,
		HandlerTimeout: config.HandlerTimeout,
		Subscriptions:  []string{config.Topic},
		Logger:         config.Logger,
	})

	svr := &HTTPServer{
		endpoint: endpoint,
		handler:  config.Handler,
	}
	endpoint.RegisterRequestHandler(config.Topic, svr.handleRequest)

	return svr
}

func (s *HTTPServer) Start(ctx context.Context) error {
	if err := s.endpoint.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	return nil
}

func (s *HTTPServer) Stop(ctx context.Context) error {
	if err := s.endpoint.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect to broker: %w", err)
	}
	return nil
}

func (p *HTTPServer) handleRequest(ctx context.Context, msg *Message) (any, error) {
	rdr := bufio.NewReader(bytes.NewBuffer(msg.Payload))
	req, err := http.ReadRequest(rdr)
	if err != nil {
		return nil, fmt.Errorf("failed to read request from payload: %w", err)
	}

	writer := newHttpResponseWriter()
	p.handler.ServeHTTP(writer, req)

	return writer.marshal()
}

// HTTPDoer is an HTTP client that communicates over MQTT. It works by reading
// and writing raw HTTP requests/responses in the MQTT message payloads.
type HTTPDoer struct {
	topic    string
	endpoint Endpoint
	maxWait  time.Duration
	qos      int
	retain   bool
}

type HTTPDoerConfig struct {
	Topic   string
	Broker  BrokerConfig
	MaxWait time.Duration
	QoS     int
	Retain  bool
}

func NewHTTPDoer(config HTTPDoerConfig) *HTTPDoer {
	endpoint := New(Config{
		Broker: config.Broker,
	})
	return &HTTPDoer{
		topic:    config.Topic,
		endpoint: endpoint,
		maxWait:  config.MaxWait,
		qos:      config.QoS,
		retain:   config.Retain,
	}
}

func (d *HTTPDoer) Connect(ctx context.Context) error {
	if err := d.endpoint.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	return nil
}

func (d *HTTPDoer) Disconnect(ctx context.Context) error {
	if err := d.endpoint.Disconnect(ctx); err != nil {
		return fmt.Errorf("failed to disconnect from broker: %w", err)
	}
	return nil
}

func (d *HTTPDoer) Do(req *http.Request) (*http.Response, error) {
	out := &bytes.Buffer{}
	if err := req.Write(out); err != nil {
		return nil, fmt.Errorf("failed to write request to wire format: %w", err)
	}

	var resp *http.Response
	if err := d.endpoint.Call(req.Context(), &Call{
		Topic:    d.topic,
		Request:  out.Bytes(),
		Response: &resp,
		QoS:      d.qos,
		MaxWait:  d.maxWait,
		Retain:   d.retain,
		Unmarshal: func(payload []byte, resp any) error {
			ptr, ok := resp.(**http.Response)
			if !ok {
				return fmt.Errorf("expected a **http.Response, got %T", resp)
			}

			// Read response from payload
			rdr := bufio.NewReader(bytes.NewBuffer(payload))
			unmarshalled, err := http.ReadResponse(rdr, req)
			if err != nil {
				return fmt.Errorf("failed to read response from wire format: %w", err)
			}

			// reassign the input response to the unmarshalled response
			*ptr = unmarshalled

			return nil

		},
	}); err != nil {
		return nil, fmt.Errorf("failed to make http call: %w", err)
	}

	return resp, nil
}

// httpResponseWriter is a custom implementation of http.httpResponseWriter.
type httpResponseWriter struct {
	header     http.Header
	body       *bytes.Buffer
	statusCode int
	written    bool
}

// newHttpResponseWriter creates a new ResponseWriter instance.
func newHttpResponseWriter() *httpResponseWriter {
	return &httpResponseWriter{
		header:     make(http.Header),
		body:       &bytes.Buffer{},
		statusCode: http.StatusOK, // Default status code
	}
}

// Header returns the response headers.
func (w *httpResponseWriter) Header() http.Header {
	return w.header
}

// Write writes the response body.
func (w *httpResponseWriter) Write(data []byte) (int, error) {
	if !w.written {
		// Ensure headers are written before the body.
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(data)
}

// WriteHeader writes the HTTP status code.
func (w *httpResponseWriter) WriteHeader(statusCode int) {
	if w.written {
		// Prevent multiple calls to WriteHeader.
		return
	}
	w.statusCode = statusCode
	w.written = true
}

func (w *httpResponseWriter) marshal() ([]byte, error) {
	res := &http.Response{
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		StatusCode:    w.statusCode,
		Header:        w.header,
		Status:        fmt.Sprintf("%03d %s", w.statusCode, http.StatusText(w.statusCode)),
		Body:          io.NopCloser(w.body),
		ContentLength: int64(w.body.Len()),
	}
	out := &bytes.Buffer{}
	if err := res.Write(out); err != nil {
		return nil, fmt.Errorf("failed to write response in wire format: %w", err)
	}

	return out.Bytes(), nil
}
