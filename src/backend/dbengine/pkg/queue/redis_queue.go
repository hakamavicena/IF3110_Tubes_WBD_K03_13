package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultQueueKey = "platform:migration_jobs"

var ErrQueueEmpty = errors.New("queue is empty")

type JobMessage struct {
	JobID       int64     `json:"job_id"`
	DatabaseID  int64     `json:"database_id"`
	TraceParent string    `json:"trace_parent,omitempty"`
	EnqueuedAt  time.Time `json:"enqueued_at"`
}

type Queue interface {
	Publish(ctx context.Context, msg JobMessage) error
	Consume(ctx context.Context, blockTimeout time.Duration) (*JobMessage, error)
	Ping(ctx context.Context) error
	Close() error
}

type RedisQueue struct {
	addr     string
	password string
	queueKey string
	dialTimeout time.Duration
}

func NewRedisQueue(addr, password, queueKey string) *RedisQueue {
	if strings.TrimSpace(addr) == "" {
		addr = "localhost:6379"
	}
	if strings.TrimSpace(queueKey) == "" {
		queueKey = DefaultQueueKey
	}
	return &RedisQueue{
		addr:        addr,
		password:    password,
		queueKey:    queueKey,
		dialTimeout: 5 * time.Second,
	}
}

func (rq *RedisQueue) Ping(ctx context.Context) error {
	conn, reader, err := rq.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeRESPCommand(conn, "PING"); err != nil {
		return err
	}
	resp, err := readRESPValue(reader)
	if err != nil {
		return err
	}
	if s, ok := resp.(string); !ok || strings.ToUpper(s) != "PONG" {
		return fmt.Errorf("unexpected redis PING response: %v", resp)
	}
	return nil
}

func (rq *RedisQueue) Publish(ctx context.Context, msg JobMessage) error {
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	conn, reader, err := rq.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := writeRESPCommand(conn, "RPUSH", rq.queueKey, string(payload)); err != nil {
		return err
	}
	_, err = readRESPValue(reader)
	return err
}

func (rq *RedisQueue) Consume(ctx context.Context, blockTimeout time.Duration) (*JobMessage, error) {
	conn, reader, err := rq.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	sec := int(blockTimeout.Seconds())
	if sec < 1 {
		sec = 1
	}
	deadline := time.Now().Add(time.Duration(sec)*time.Second + 3*time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := writeRESPCommand(conn, "BLPOP", rq.queueKey, strconv.Itoa(sec)); err != nil {
		return nil, err
	}
	val, err := readRESPValue(reader)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, ErrQueueEmpty
	}

	arr, ok := val.([]any)
	if !ok || len(arr) < 2 {
		return nil, ErrQueueEmpty
	}
	rawJSON, ok := arr[1].(string)
	if !ok {
		return nil, fmt.Errorf("invalid redis BLPOP payload type")
	}

	var msg JobMessage
	if err := json.Unmarshal([]byte(rawJSON), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (rq *RedisQueue) Close() error {
	return nil
}

func (rq *RedisQueue) dial(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	dialer := net.Dialer{Timeout: rq.dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", rq.addr)
	if err != nil {
		return nil, nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	reader := bufio.NewReader(conn)
	if rq.password != "" {
		if err := writeRESPCommand(conn, "AUTH", rq.password); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		if _, err := readRESPValue(reader); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}
	return conn, reader, nil
}

func writeRESPCommand(w io.Writer, args ...string) error {
	var b strings.Builder
	b.WriteString("*")
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteString("\r\n")
	for _, arg := range args {
		b.WriteString("$")
		b.WriteString(strconv.Itoa(len(arg)))
		b.WriteString("\r\n")
		b.WriteString(arg)
		b.WriteString("\r\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func readRESPValue(r *bufio.Reader) (any, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, fmt.Errorf("empty RESP line")
	}

	prefix := line[0]
	body := line[1:]

	switch prefix {
	case '+':
		return body, nil
	case '-':
		return nil, fmt.Errorf("redis error: %s", body)
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	case '$':
		size, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, nil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		count, err := strconv.Atoi(body)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, nil
		}
		items := make([]any, 0, count)
		for i := 0; i < count; i++ {
			item, err := readRESPValue(r)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("unknown RESP prefix %q", prefix)
	}
}

type MemoryQueue struct {
	mu     sync.Mutex
	items  []JobMessage
	notify chan struct{}
	closed bool
}

func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{
		notify: make(chan struct{}, 1),
	}
}

func (mq *MemoryQueue) Ping(_ context.Context) error {
	return nil
}

func (mq *MemoryQueue) Publish(_ context.Context, msg JobMessage) error {
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = time.Now().UTC()
	}
	mq.mu.Lock()
	if mq.closed {
		mq.mu.Unlock()
		return fmt.Errorf("memory queue is closed")
	}
	mq.items = append(mq.items, msg)
	mq.mu.Unlock()

	select {
	case mq.notify <- struct{}{}:
	default:
	}
	return nil
}

func (mq *MemoryQueue) Consume(ctx context.Context, blockTimeout time.Duration) (*JobMessage, error) {
	if msg := mq.tryPop(); msg != nil {
		return msg, nil
	}
	if blockTimeout <= 0 {
		blockTimeout = 50 * time.Millisecond
	}
	timer := time.NewTimer(blockTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		if msg := mq.tryPop(); msg != nil {
			return msg, nil
		}
		return nil, ErrQueueEmpty
	case <-mq.notify:
		if msg := mq.tryPop(); msg != nil {
			return msg, nil
		}
		return nil, ErrQueueEmpty
	}
}

func (mq *MemoryQueue) tryPop() *JobMessage {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	if len(mq.items) == 0 {
		return nil
	}
	msg := mq.items[0]
	mq.items = mq.items[1:]
	if len(mq.items) > 0 {
		select {
		case mq.notify <- struct{}{}:
		default:
		}
	}
	return &msg
}

func (mq *MemoryQueue) Len() int {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	return len(mq.items)
}

func (mq *MemoryQueue) Close() error {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	mq.closed = true
	return nil
}
