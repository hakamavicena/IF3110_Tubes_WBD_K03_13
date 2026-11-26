package queue

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestMemoryQueueFIFOAndEmptyTimeout(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	defer q.Close()

	first := JobMessage{JobID: 101, DatabaseID: 1, TraceParent: "00-trace-1"}
	second := JobMessage{JobID: 102, DatabaseID: 2, TraceParent: "00-trace-2"}

	if err := q.Publish(ctx, first); err != nil {
		t.Fatalf("Publish first failed: %v", err)
	}
	if err := q.Publish(ctx, second); err != nil {
		t.Fatalf("Publish second failed: %v", err)
	}
	if q.Len() != 2 {
		t.Fatalf("expected queue length 2, got %d", q.Len())
	}

	got1, err := q.Consume(ctx, 50*time.Millisecond)
	if err != nil || got1 == nil {
		t.Fatalf("Consume first failed: %v", err)
	}
	if got1.JobID != 101 || got1.DatabaseID != 1 || got1.TraceParent != "00-trace-1" {
		t.Fatalf("unexpected first message: %+v", got1)
	}

	got2, err := q.Consume(ctx, 50*time.Millisecond)
	if err != nil || got2 == nil {
		t.Fatalf("Consume second failed: %v", err)
	}
	if got2.JobID != 102 || got2.DatabaseID != 2 {
		t.Fatalf("unexpected second message: %+v", got2)
	}

	_, err = q.Consume(ctx, 20*time.Millisecond)
	if !errors.Is(err, ErrQueueEmpty) {
		t.Fatalf("expected ErrQueueEmpty on empty queue, got %v", err)
	}
}

func TestRESPCommandAndValueRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := writeRESPCommand(&buf, "RPUSH", "platform:migration_jobs", `{"job_id":7}`); err != nil {
		t.Fatalf("writeRESPCommand failed: %v", err)
	}
	want := "*3\r\n$5\r\nRPUSH\r\n$23\r\nplatform:migration_jobs\r\n$12\r\n{\"job_id\":7}\r\n"
	if buf.String() != want {
		t.Fatalf("unexpected RESP wire format:\ngot:  %q\nwant: %q", buf.String(), want)
	}

	reader := bufio.NewReader(strings.NewReader("*2\r\n$23\r\nplatform:migration_jobs\r\n$29\r\n{\"job_id\":7,\"database_id\":15}\r\n"))
	val, err := readRESPValue(reader)
	if err != nil {
		t.Fatalf("readRESPValue failed: %v", err)
	}
	arr, ok := val.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2-element RESP array, got %#v", val)
	}
	if arr[1] != `{"job_id":7,"database_id":15}` {
		t.Fatalf("unexpected payload element: %v", arr[1])
	}
}

func TestRedisQueueAgainstMockRESPServer(t *testing.T) {
	stored := make(chan string, 1)

	rq := NewRedisQueue("mock:6379", "", "platform:migration_jobs")
	rq.dialFunc = func(_ context.Context, _, _ string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go func(c net.Conn) {
			defer c.Close()
			r := bufio.NewReader(c)
			val, err := readRESPValue(r)
			if err != nil {
				return
			}
			arr, ok := val.([]any)
			if !ok || len(arr) == 0 {
				return
			}
			cmd, _ := arr[0].(string)
			switch strings.ToUpper(cmd) {
			case "PING":
				_, _ = c.Write([]byte("+PONG\r\n"))
			case "RPUSH":
				if len(arr) >= 3 {
					payload, _ := arr[2].(string)
					stored <- payload
				}
				_, _ = c.Write([]byte(":1\r\n"))
			case "BLPOP":
				select {
				case p := <-stored:
					_ = writeRESPCommand(c, "platform:migration_jobs", p)
				default:
					_, _ = c.Write([]byte("*-1\r\n"))
				}
			}
		}(serverConn)
		return clientConn, nil
	}

	ctx := context.Background()

	if err := rq.Ping(ctx); err != nil {
		t.Fatalf("Ping mock redis failed: %v", err)
	}
	if err := rq.Publish(ctx, JobMessage{JobID: 55, DatabaseID: 9}); err != nil {
		t.Fatalf("Publish mock redis failed: %v", err)
	}
	msg, err := rq.Consume(ctx, 1*time.Second)
	if err != nil || msg == nil {
		t.Fatalf("Consume mock redis failed: %v", err)
	}
	if msg.JobID != 55 || msg.DatabaseID != 9 {
		t.Fatalf("unexpected consumed message: %+v", msg)
	}
}
