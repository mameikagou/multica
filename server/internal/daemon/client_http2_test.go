package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type blackholeConn struct {
	net.Conn
	drop atomic.Bool
}

func (c *blackholeConn) Read(buf []byte) (int, error) {
	for {
		n, err := c.Conn.Read(buf)
		if err != nil || !c.drop.Load() {
			return n, err
		}
	}
}

func (c *blackholeConn) Write(buf []byte) (int, error) {
	if c.drop.Load() {
		return len(buf), nil
	}
	return c.Conn.Write(buf)
}

type blackholeListener struct {
	net.Listener
	accepted chan *blackholeConn
}

func (l *blackholeListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &blackholeConn{Conn: conn}
	l.accepted <- wrapped
	return wrapped, nil
}

func newHTTP2TestClient(t *testing.T, handler http.Handler) (*Client, <-chan *blackholeConn) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	listener := &blackholeListener{Listener: server.Listener, accepted: make(chan *blackholeConn, 16)}
	server.Listener = listener
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	client := NewClient(server.URL)
	transport := client.client.Transport.(*http.Transport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	// 缩短已启用的健康检查间隔；未配置的客户端仍保持原行为，用于验证回归。
	if transport.HTTP2 != nil {
		transport.HTTP2.SendPingTimeout /= 100
		transport.HTTP2.PingTimeout /= 100
	}
	client.client.Timeout = 2 * time.Second
	t.Cleanup(client.CloseIdleConnections)
	return client, listener.accepted
}

func TestClientHTTP2RecoversBlackholedConnection(t *testing.T) {
	started := make(chan struct{}, 1)
	var posts atomic.Int32
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := client.getJSON(ctx, "/warm", nil); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	result := make(chan error, 1)
	go func() { result <- client.ReportProgress(ctx, "task", "running", 1, 2) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("POST did not reach server")
	}
	// 在真实 TLS/HTTP2 连接上丢弃双向数据，保留 TCP 连接和正在执行的请求。
	conn.drop.Store(true)
	if err := <-result; err == nil {
		t.Fatal("interrupted POST unexpectedly succeeded")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active stream waited for request timeout instead of connection recovery: %v", err)
	}
	if err := client.getJSON(ctx, "/recovered", nil); err != nil {
		t.Fatalf("client reused the dead connection: %v", err)
	}
	select {
	case <-connections:
	default:
		t.Fatal("recovery did not open a new connection")
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("interrupted POST was replayed: received %d calls", got)
	}
}

func TestClientHTTP2KeepsHealthySlowConnection(t *testing.T) {
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	for _, path := range []string{"/warm", "/slow", "/after"} {
		if err := client.getJSON(context.Background(), path, nil); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	<-connections
	select {
	case <-connections:
		t.Fatal("healthy connection was replaced while waiting for a response")
	default:
	}
}

func TestClientHTTP1WithConnectionHealthChecks(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Errorf("expected HTTP/1, got %s", r.Proto)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	client := NewClient(server.URL)
	defer client.CloseIdleConnections()
	client.client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if err := client.getJSON(context.Background(), "/", nil); err != nil {
		t.Fatal(err)
	}
}
