package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Endpoint interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Call(ctx context.Context, c *Call) error
	Publish(ctx context.Context, msg *Message) error
	Subscribe(ctx context.Context, topic string) error
	Unsubscribe(ctx context.Context, topic string) error
	RegisterMessageHandler(topic string, h MessageHandler)
	RegisterRequestHandler(topic string, h RequestHandler)
	UnregisterHandler(topic string)
}

var _ Endpoint = (*MQTTEndpoint)(nil)

type MQTTEndpoint struct {
	logger         *zerolog.Logger
	router         paho.Router
	url            string
	username       string
	password       string
	cm             *autopaho.ConnectionManager
	subs           map[string]bool
	responseTopic  string
	responseChans  map[string]chan *paho.Publish
	mutex          sync.Mutex
	subsMutex      sync.Mutex
	clientID       string
	subsErr        error
	handlerTimeout time.Duration
}

type Config struct {
	Logger          *zerolog.Logger
	URL             string
	ClientID        string
	Username        string
	Password        string
	MessageHandlers map[string]MessageHandler
	RequestHandlers map[string]RequestHandler
	HandlerTimeout  time.Duration
	Subscriptions   []string
	AutoSubscribe   bool
}

type Message struct {
	Topic   string
	QoS     int
	Payload []byte
}

type PublishFunc func(ctx context.Context, msg *Message) error

type MessageHandler func(ctx context.Context, msg *Message)

type RequestHandler func(ctx context.Context, msg *Message) (interface{}, error)

type Call struct {
	Topic    string
	Request  interface{}
	Response interface{}
	QoS      int
	MaxWait  time.Duration
}

func (c *Call) Payload() ([]byte, error) {
	if c.Request == nil {
		return []byte{}, nil
	}
	if value, ok := c.Request.([]byte); ok {
		return value, nil
	}
	return json.Marshal(c.Request)
}

func New(config Config) (*MQTTEndpoint, error) {
	clientID := config.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("mqtt-%s", uuid.New())
	}
	respTopic := fmt.Sprintf("rsp/callers/%s", clientID)
	handlerTimeout := config.HandlerTimeout
	if handlerTimeout == 0 {
		handlerTimeout = time.Minute
	}
	url := config.URL
	if url == "" {
		return nil, errors.New("url is required")
	}
	if !strings.HasPrefix(url, "tls://") && !strings.HasPrefix(url, "tcp://") {
		url = fmt.Sprintf("tls://%s:8883", url)
	}
	e := &MQTTEndpoint{
		logger:         config.Logger,
		url:            url,
		clientID:       clientID,
		responseTopic:  respTopic,
		responseChans:  make(map[string]chan *paho.Publish),
		username:       config.Username,
		password:       config.Password,
		handlerTimeout: handlerTimeout,
		subs:           map[string]bool{},
	}
	e.router = paho.NewStandardRouterWithDefault(e.defaultHandler)
	for _, topic := range config.Subscriptions {
		e.subs[topic] = true
	}
	// Message handlers
	for topic, handler := range config.MessageHandlers {
		e.RegisterMessageHandler(topic, handler)
		if config.AutoSubscribe {
			e.subs[topic] = true
		}
	}
	// Request handlers
	for topic, handler := range config.RequestHandlers {
		e.RegisterRequestHandler(topic, handler)
		if config.AutoSubscribe {
			e.subs[topic] = true
		}
	}
	// Response handler
	e.router.RegisterHandler(respTopic, e.handleResponse)
	e.subs[respTopic] = true
	if e.logger == nil {
		logger := zerolog.New(os.Stdout).With().Timestamp().Caller().Logger()
		e.logger = &logger
	}
	return e, nil
}

func (e *MQTTEndpoint) Connect(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	var err error
	if e.cm != nil {
		return errors.New("mqtt endpoint is already connected")
	}
	e.subsErr = nil
	brokerURL, err := url.Parse(e.url)
	if err != nil {
		return fmt.Errorf("failed to parse mqtt url: %v", err)
	}
	readyChan := make(chan struct{})
	e.cm, err = autopaho.NewConnection(ctx, e.getPahoConfig(brokerURL, readyChan))
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to create connection to mqtt broker")
		return err
	}
	if err = e.cm.AwaitConnection(ctx); err != nil {
		e.logger.Error().Err(err).Msg("failed to connect to mqtt broker")
		return err
	}
	<-readyChan // Wait for the subscriptions to be ready
	if e.subsErr != nil {
		e.logger.Error().Err(err).Msg("failed to create mqtt subscriptions")
		return e.subsErr
	}
	e.logger.Info().
		Str("url", e.url).
		Str("username", e.username).
		Str("client_id", e.clientID).
		Str("response_topic", e.responseTopic).
		Msg("connected to mqtt broker")
	return nil
}

func (e *MQTTEndpoint) Disconnect(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.cm == nil {
		return errors.New("mqtt endpoint is not connected")
	}
	err := e.cm.Disconnect(ctx)
	<-e.cm.Done()
	e.cm = nil
	return err
}

func (e *MQTTEndpoint) publish(ctx context.Context, msg *paho.Publish) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.cm == nil {
		return errors.New("failed to publish message: not connected")
	}
	if _, err := e.cm.Publish(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish message: %s", err)
	}
	return nil
}

func (e *MQTTEndpoint) handleRequest(msg *paho.Publish, h RequestHandler) {
	logger := e.logger.With().
		Str("topic", msg.Topic).
		Str("correlation_id", string(msg.Properties.CorrelationData)).
		Logger()
	handlerCtx := context.Background()
	handlerCtx = logger.WithContext(handlerCtx)
	handlerCtx = WithPublishFunc(handlerCtx, func(ctx context.Context, msg *Message) error {
		return e.Publish(ctx, msg)
	})
	// response should be asynchronous - don't block the handler
	go func() {
		result, handlerErr := h(handlerCtx, &Message{
			Topic:   msg.Topic,
			QoS:     int(msg.QoS),
			Payload: msg.Payload,
		})
		response, err := makeResponse(msg, result, handlerErr)
		if err != nil {
			logger.Err(err).Msg("failed to build response")
			return
		}
		if err := e.publish(handlerCtx, response); err != nil {
			logger.Err(err).Msg("failed to send response")
			return
		}
	}()
}

func (e *MQTTEndpoint) defaultHandler(msg *paho.Publish) {
	ctx, cancel := context.WithTimeout(context.Background(), e.handlerTimeout)
	defer cancel()

	logger := e.logger.With().
		Str("topic", msg.Topic).
		Str("correlation_id", string(msg.Properties.CorrelationData)).
		Logger()

	logger.Error().Msg("unsupported operation")

	response, err := makeErrorResponse(msg, ErrUnsupported)
	if err != nil {
		logger.Err(err).Msg("failed to build response")
		return
	}
	if err := e.publish(ctx, response); err != nil {
		logger.Err(err).Msg("failed to send response")
		return
	}
}

// Call sends a command and blocks until a response is received. Requests and
// responses are automatically marshalled to/from JSON. The response is expected
// to be a JSON object with either `result` or `error` properties set.
func (e *MQTTEndpoint) Call(ctx context.Context, c *Call) error {
	if c.Topic == "" {
		return errors.New("topic is required")
	}
	payload, err := c.Payload()
	if err != nil {
		return err
	}
	maxWait := time.Minute * 10
	if c.MaxWait > 0 {
		maxWait = c.MaxWait
	}
	qos := byte(c.QoS)
	if qos == 0 {
		qos = 1
	}
	resp, err := e.executeCall(ctx, &paho.Publish{
		Payload: payload,
		Topic:   c.Topic,
		QoS:     qos,
	}, maxWait)
	if err != nil {
		return err
	}
	// Unwrap either the result or the error
	type responseWrapper struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	var wrapper responseWrapper
	if err := json.Unmarshal(resp.Payload, &wrapper); err != nil {
		return err
	}
	if len(wrapper.Error) > 0 {
		var errStr string
		if err := json.Unmarshal(wrapper.Error, &errStr); err != nil {
			return NewRpcError(string(wrapper.Error))
		}
		return NewRpcError(errStr)
	}
	if err := json.Unmarshal(wrapper.Result, &c.Response); err != nil {
		return err
	}
	return nil
}

func (e *MQTTEndpoint) executeCall(ctx context.Context, pub *paho.Publish, maxWait time.Duration) (*paho.Publish, error) {
	// Create a dedicated response channel for the call
	correlationID := uuid.NewString()
	responseChan := make(chan *paho.Publish)
	e.mutex.Lock()
	e.responseChans[correlationID] = responseChan
	e.mutex.Unlock()
	defer func() {
		e.mutex.Lock()
		delete(e.responseChans, correlationID)
		e.mutex.Unlock()
	}()
	// Send the request message with properties that allow a response
	pub.Properties = &paho.PublishProperties{
		ResponseTopic:   e.responseTopic,
		CorrelationData: []byte(correlationID),
	}
	if err := e.publish(ctx, pub); err != nil {
		return nil, err
	}
	e.logger.Debug().
		Str("topic", pub.Topic).
		Str("correlation_id", correlationID).
		Str("response_topic", e.responseTopic).
		Msg("sent request")
	// Wait for a response. While waiting, check for context cancellation
	// and discard any messages that don't match the correlation ID.
	for {
		select {
		case <-time.After(maxWait):
			e.logger.Warn().
				Str("correlation_id", correlationID).
				Msg("timeout while waiting on response")
			return nil, errors.New("timeout waiting for response")
		case <-ctx.Done():
			e.logger.Warn().
				Str("correlation_id", correlationID).
				Msg("context done while waiting for response")
			return nil, ctx.Err()
		case resp := <-responseChan:
			rxCorr := string(resp.Properties.CorrelationData)
			if rxCorr == correlationID {
				return resp, nil
			}
			e.logger.Warn().
				Str("correlation_id", correlationID).
				Str("received_correlation_id", rxCorr).
				Msg("dropping response with unexpected correlation id")
		}
	}
}

func (e *MQTTEndpoint) handleResponse(msg *paho.Publish) {
	// There should be an executeCall call in progress that's waiting for
	// this response message. Pass the message via the matching responseChan.
	e.mutex.Lock()
	defer e.mutex.Unlock()
	correlationID := string(msg.Properties.CorrelationData)
	responseChan, ok := e.responseChans[correlationID]
	if !ok {
		e.logger.Warn().Str("correlation_id", correlationID).
			Msg("dropping response due to unknown correlation id")
		return
	}
	select {
	case responseChan <- msg:
		e.logger.Debug().
			Str("correlation_id", correlationID).
			Str("topic", msg.Topic).
			Msg("received response")
	default:
		e.logger.Warn().Str("topic", msg.Topic).Msg("dropping response")
	}
}

// Publish a message to a specified topic. The payload is automatically
// marshalled to JSON if it's not already a byte slice.
func (e *MQTTEndpoint) Publish(ctx context.Context, msg *Message) error {
	return e.publish(ctx, &paho.Publish{
		Topic:   msg.Topic,
		Payload: msg.Payload,
		QoS:     byte(msg.QoS),
	})
}

func (e *MQTTEndpoint) Subscribe(ctx context.Context, topic string) error {
	e.subsMutex.Lock()
	defer e.subsMutex.Unlock()
	_, err := e.cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: topic, QoS: byte(1)},
		},
	})
	if err == nil {
		e.subs[topic] = true
	}
	return err
}

func (e *MQTTEndpoint) Unsubscribe(ctx context.Context, topic string) error {
	e.subsMutex.Lock()
	defer e.subsMutex.Unlock()
	_, err := e.cm.Unsubscribe(ctx, &paho.Unsubscribe{Topics: []string{topic}})
	if err == nil {
		delete(e.subs, topic)
	}
	return err
}

func (e *MQTTEndpoint) RegisterMessageHandler(topic string, h MessageHandler) {
	e.router.RegisterHandler(topic, func(msg *paho.Publish) {
		ctx, cancel := context.WithTimeout(context.Background(), e.handlerTimeout)
		defer cancel()
		h(ctx, &Message{Topic: msg.Topic, QoS: int(msg.QoS), Payload: msg.Payload})
	})
}

func (e *MQTTEndpoint) RegisterRequestHandler(topic string, h RequestHandler) {
	e.router.RegisterHandler(topic, func(msg *paho.Publish) {
		e.handleRequest(msg, h)
	})
}

func (e *MQTTEndpoint) UnregisterHandler(topic string) {
	e.router.UnregisterHandler(topic)
}

func (e *MQTTEndpoint) getPahoConfig(brokerURL *url.URL, readyChan chan struct{}) autopaho.ClientConfig {
	var once sync.Once
	cfg := autopaho.ClientConfig{
		ServerUrls:        []*url.URL{brokerURL},
		ConnectUsername:   e.username,
		ConnectPassword:   []byte(e.password),
		KeepAlive:         30,
		ConnectRetryDelay: 10 * time.Second,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			e.subsMutex.Lock()
			subscriptions := make([]paho.SubscribeOptions, len(e.subs))
			idx := 0
			for topic := range e.subs {
				subscriptions[idx] = paho.SubscribeOptions{
					Topic: topic,
					QoS:   byte(1),
				}
				idx++
			}
			e.subsMutex.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
			defer cancel()
			if _, err := cm.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: subscriptions,
			}); err != nil {
				e.logger.Error().Err(err).
					Interface("subscribed_topics", e.subs).
					Str("response_topic", e.responseTopic).
					Msg("failed to subscribe to topics")
				e.subsErr = err
			} else {
				e.logger.Debug().
					Interface("subscribed_topics", e.subs).
					Str("response_topic", e.responseTopic).
					Msg("subscribed to topics")
			}
			// Signal that the subscriptions are ready. Note that this callback
			// could be called multiple times if reconnections occur, hence we
			// use a sync.Once to ensure that the readyChan is only closed once.
			once.Do(func() { close(readyChan) })
		},
		ClientConfig: paho.ClientConfig{
			ClientID: e.clientID,
			Router:   e.router,
			OnClientError: func(err error) {
				e.logger.Warn().Err(err).Msg("mqtt client error")
			},
			OnServerDisconnect: func(d *paho.Disconnect) {
				if d.Properties != nil {
					e.logger.Warn().
						Str("reason", d.Properties.ReasonString).
						Interface("code", d.ReasonCode).
						Msg("server requested disconnect")
				} else {
					e.logger.Warn().
						Int("code", int(d.ReasonCode)).
						Msg("server requested disconnect")
				}
				time.Sleep(time.Second * 10)
			},
		},
	}
	return cfg
}

func makeErrorResponse(req *paho.Publish, err error) (*paho.Publish, error) {
	payload, err := json.Marshal(map[string]string{"error": err.Error()})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal error response: %v", err)
	}
	return &paho.Publish{
		Topic:      req.Properties.ResponseTopic,
		Properties: &paho.PublishProperties{CorrelationData: req.Properties.CorrelationData},
		Payload:    payload,
		QoS:        1,
	}, nil
}

func makeResponse(req *paho.Publish, resp interface{}, err error) (*paho.Publish, error) {
	if err != nil {
		return makeErrorResponse(req, err)
	}
	type wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	var w wrapper
	w.Result, err = getBytes(resp)
	if err != nil {
		// At this point, the result value could not be marshalled to JSON.
		// Switch to sending an error response to indicate this instead.
		resp, err := makeErrorResponse(req, err)
		if err != nil {
			// We should never reach this point
			return nil, err
		}
		return resp, nil
	}
	payload, err := json.Marshal(w)
	if err != nil {
		// We should never reach this point
		return nil, fmt.Errorf("failed to marshal response: %v", err)
	}
	return &paho.Publish{
		Topic:      req.Properties.ResponseTopic,
		Properties: &paho.PublishProperties{CorrelationData: req.Properties.CorrelationData},
		Payload:    payload,
		QoS:        1,
	}, nil
}

func getBytes(value interface{}) ([]byte, error) {
	if bytes, ok := value.([]byte); ok {
		return bytes, nil
	}
	return json.Marshal(value)
}
