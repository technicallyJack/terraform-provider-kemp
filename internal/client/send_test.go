package client

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// dropListener closes the first n accepted connections before the TLS
// handshake, the way the LoadMaster does when overloaded.
type dropListener struct {
	net.Listener
	drop atomic.Int32
}

func (l *dropListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || l.drop.Add(-1) < 0 {
			return conn, err
		}
		conn.Close()
	}
}

func TestSendRetriesDroppedConnections(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":200,"status":"ok"}`))
	}))
	l := &dropListener{Listener: srv.Listener}
	l.drop.Store(2)
	srv.Listener = l
	srv.StartTLS()
	defer srv.Close()

	orig := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { retryDelays = orig }()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	if err := c.Do(context.Background(), "addrule", nil, nil); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want exactly 1", calls.Load())
	}
}

func TestSendDoesNotRetryAfterRequestWritten(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		// Drop the connection after reading the request: the write may have happened.
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true})
	if err := c.Do(context.Background(), "addrule", nil, nil); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("request sent %d times, want 1 (no retry once written)", calls.Load())
	}
}

func TestSendLimitsConcurrency(t *testing.T) {
	var inFlight, peak atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		_, _ = w.Write([]byte(`{"code":200,"status":"ok"}`))
	}))
	defer srv.Close()

	c, _ := New(Config{Host: srv.URL, APIKey: "k", Insecure: true, MaxConcurrentRequests: 3})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Do(context.Background(), "listvs", nil, nil)
		}()
	}
	wg.Wait()
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency %d, want <= 3", peak.Load())
	}
}
