package rabbitmq

import (
	"ai-chat/config"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

const (
	defaultMaxRetryCount = 3
	defaultRetryDelayMS  = 5000
	publishRetryAttempts = 3
	consumePrefetch      = 16
	retryHeaderKey       = "x-retry-count"
)

type RabbitMQ struct {
	Exchange string
	Key      string

	uri           string
	retryQueue    string
	deadLetterQ   string
	maxRetryCount int
	retryDelayMS  int

	conn       *amqp.Connection
	pubChannel *amqp.Channel
	conChannel *amqp.Channel

	mu      sync.RWMutex
	closing chan struct{}
}

func NewRabbitMQ(exchange, key string) *RabbitMQ {
	return &RabbitMQ{
		Exchange: exchange,
		Key:      key,
		closing:  make(chan struct{}),
	}
}

func (r *RabbitMQ) Destroy() {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-r.closing:
	default:
		close(r.closing)
	}
	r.closeResourcesLocked()
}

func NewWorkRabbitMQ(queue string) *RabbitMQ {
	c := config.GetConfig()
	mqURL := fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		c.RabbitmqUsername, c.RabbitmqPassword, c.RabbitmqHost, c.RabbitmqPort, c.RabbitmqVhost,
	)

	maxRetry := c.RabbitmqRetryMax
	if maxRetry <= 0 {
		maxRetry = defaultMaxRetryCount
	}
	retryDelayMS := c.RetryDelayMs
	if retryDelayMS <= 0 {
		retryDelayMS = defaultRetryDelayMS
	}

	r := NewRabbitMQ("", queue)
	r.uri = mqURL
	r.retryQueue = queue + ".retry"
	r.deadLetterQ = queue + ".dlq"
	r.maxRetryCount = maxRetry
	r.retryDelayMS = retryDelayMS

	if err := r.connect(); err != nil {
		log.Fatalf("failed to initialize RabbitMQ: %v", err)
	}
	return r
}

func (r *RabbitMQ) connect() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reconnectLocked()
}

func (r *RabbitMQ) reconnectLocked() error {
	r.closeResourcesLocked()

	conn, err := amqp.Dial(r.uri)
	if err != nil {
		return fmt.Errorf("dial rabbitmq failed: %w", err)
	}

	pubCh, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open publish channel failed: %w", err)
	}

	conCh, err := conn.Channel()
	if err != nil {
		_ = pubCh.Close()
		_ = conn.Close()
		return fmt.Errorf("open consume channel failed: %w", err)
	}

	if err = conCh.Qos(consumePrefetch, 0, false); err != nil {
		_ = conCh.Close()
		_ = pubCh.Close()
		_ = conn.Close()
		return fmt.Errorf("set consume qos failed: %w", err)
	}

	r.conn = conn
	r.pubChannel = pubCh
	r.conChannel = conCh

	if err = r.declareTopologyLocked(); err != nil {
		r.closeResourcesLocked()
		return err
	}

	log.Printf("rabbitmq connected, queue=%s retry=%s dlq=%s", r.Key, r.retryQueue, r.deadLetterQ)
	return nil
}

func (r *RabbitMQ) declareTopologyLocked() error {
	if r.pubChannel == nil {
		return errors.New("publish channel is nil")
	}

	if _, err := r.pubChannel.QueueDeclare(
		r.Key,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare main queue failed: %w", err)
	}

	retryArgs := amqp.Table{
		"x-message-ttl":             int32(r.retryDelayMS),
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": r.Key,
	}
	if _, err := r.pubChannel.QueueDeclare(
		r.retryQueue,
		true,
		false,
		false,
		false,
		retryArgs,
	); err != nil {
		return fmt.Errorf("declare retry queue failed: %w", err)
	}

	if _, err := r.pubChannel.QueueDeclare(
		r.deadLetterQ,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare dead-letter queue failed: %w", err)
	}

	return nil
}

func (r *RabbitMQ) closeResourcesLocked() {
	if r.pubChannel != nil {
		_ = r.pubChannel.Close()
		r.pubChannel = nil
	}
	if r.conChannel != nil {
		_ = r.conChannel.Close()
		r.conChannel = nil
	}
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
}

func (r *RabbitMQ) ensureConnected() error {
	r.mu.RLock()
	ready := r.conn != nil && !r.conn.IsClosed() && r.pubChannel != nil && r.conChannel != nil
	r.mu.RUnlock()
	if ready {
		return nil
	}
	return r.connect()
}

func (r *RabbitMQ) withReconnect(op func() error) error {
	var lastErr error
	for i := 0; i < publishRetryAttempts; i++ {
		if err := r.ensureConnected(); err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}

		if err := op(); err != nil {
			lastErr = err
			r.mu.Lock()
			r.closeResourcesLocked()
			r.mu.Unlock()
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("unknown rabbitmq error")
	}
	return lastErr
}

func (r *RabbitMQ) Publish(message []byte) error {
	return r.publishWithHeaders(r.Key, message, nil)
}

func (r *RabbitMQ) publishWithHeaders(queue string, body []byte, headers amqp.Table) error {
	return r.withReconnect(func() error {
		r.mu.RLock()
		defer r.mu.RUnlock()
		if r.pubChannel == nil {
			return errors.New("publish channel is nil")
		}
		return r.pubChannel.Publish(
			r.Exchange,
			queue,
			false,
			false,
			amqp.Publishing{
				ContentType:  "application/json",
				DeliveryMode: amqp.Persistent,
				Body:         body,
				Headers:      headers,
				Timestamp:    time.Now(),
			},
		)
	})
}

func (r *RabbitMQ) Consume(handle func(msg *amqp.Delivery) error) {
	backoff := time.Second
	for {
		select {
		case <-r.closing:
			return
		default:
		}

		if err := r.ensureConnected(); err != nil {
			log.Printf("rabbitmq consume ensureConnected failed: %v", err)
			time.Sleep(backoff)
			backoff = minDuration(backoff*2, 10*time.Second)
			continue
		}

		msgs, closeCh, err := r.openConsumer()
		if err != nil {
			log.Printf("rabbitmq open consumer failed: %v", err)
			r.mu.Lock()
			r.closeResourcesLocked()
			r.mu.Unlock()
			time.Sleep(backoff)
			backoff = minDuration(backoff*2, 10*time.Second)
			continue
		}

		backoff = time.Second
		if err := r.consumeLoop(msgs, closeCh, handle); err != nil {
			log.Printf("rabbitmq consume loop interrupted: %v", err)
		}

		r.mu.Lock()
		r.closeResourcesLocked()
		r.mu.Unlock()
		time.Sleep(backoff)
	}
}

func (r *RabbitMQ) openConsumer() (<-chan amqp.Delivery, <-chan *amqp.Error, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.conChannel == nil || r.conn == nil || r.conn.IsClosed() {
		return nil, nil, errors.New("consumer channel is not ready")
	}

	msgs, err := r.conChannel.Consume(r.Key, "", false, false, false, false, nil)
	if err != nil {
		return nil, nil, err
	}
	closeCh := r.conn.NotifyClose(make(chan *amqp.Error, 1))
	return msgs, closeCh, nil
}

func (r *RabbitMQ) consumeLoop(msgs <-chan amqp.Delivery, closeCh <-chan *amqp.Error, handle func(msg *amqp.Delivery) error) error {
	for {
		select {
		case <-r.closing:
			return nil
		case err := <-closeCh:
			if err == nil {
				return errors.New("rabbitmq connection closed")
			}
			return err
		case msg, ok := <-msgs:
			if !ok {
				return errors.New("rabbitmq delivery channel closed")
			}
			r.handleDelivery(&msg, handle)
		}
	}
}

func (r *RabbitMQ) handleDelivery(msg *amqp.Delivery, handle func(msg *amqp.Delivery) error) {
	if err := handle(msg); err != nil {
		retryCount := getRetryCount(msg.Headers)
		if retryCount < r.maxRetryCount {
			headers := cloneHeaders(msg.Headers)
			headers[retryHeaderKey] = retryCount + 1
			if pubErr := r.publishWithHeaders(r.retryQueue, msg.Body, headers); pubErr != nil {
				log.Printf("publish retry message failed: %v", pubErr)
				_ = msg.Nack(false, true)
				return
			}
			log.Printf("message moved to retry queue, retry=%d, err=%v", retryCount+1, err)
		} else {
			headers := cloneHeaders(msg.Headers)
			headers["x-last-error"] = err.Error()
			if pubErr := r.publishWithHeaders(r.deadLetterQ, msg.Body, headers); pubErr != nil {
				log.Printf("publish dead-letter message failed: %v", pubErr)
				_ = msg.Nack(false, true)
				return
			}
			log.Printf("message moved to dead-letter queue after %d retries, err=%v", retryCount, err)
		}
		_ = msg.Ack(false)
		return
	}

	if err := msg.Ack(false); err != nil {
		log.Printf("ack message failed: %v", err)
	}
}

func getRetryCount(headers amqp.Table) int {
	if headers == nil {
		return 0
	}
	raw, ok := headers[retryHeaderKey]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint8:
		return int(v)
	default:
		return 0
	}
}

func cloneHeaders(headers amqp.Table) amqp.Table {
	out := amqp.Table{}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
