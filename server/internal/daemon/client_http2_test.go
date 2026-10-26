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
	for _, httpClient := range []*http.Client{client.client, client.bundleClient} {
		if httpClient.Transport == nil {
			httpClient.Transport = http.DefaultTransport.(*http.Transport).Clone()
		}
		transport := httpClient.Transport.(*http.Transport)
		transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		if transport.HTTP2 != nil {
			transport.HTTP2.SendPingTimeout /= 100
			transport.HTTP2.PingTimeout /= 100
		}
		t.Cleanup(httpClient.CloseIdleConnections)
	}
	client.client.Timeout = 2 * time.Second
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

func TestResolveSkillBundleHTTP2RecoversBlackholedConnection(t *testing.T) {
	started := make(chan struct{}, 1)
	var requests atomic.Int32
	const response = `{"bundles":[{"id":"skill","content":"skill instructions"}]}`
	client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if requests.Add(1) == 2 {
			_, _ = io.WriteString(w, `{"bundles":[`)
			w.(http.Flusher).Flush()
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, response)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	ref := SkillRefData{ID: "skill"}
	if _, _, err := client.ResolveSkillBundle(ctx, "runtime", "task", ref); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	type result struct {
		bundle SkillData
		stats  TransferStats
		err    error
	}
	done := make(chan result, 1)
	go func() {
		bundle, stats, err := client.ResolveSkillBundle(ctx, "runtime", "task", ref)
		done <- result{bundle, stats, err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("bundle request did not reach server")
	}
	conn.drop.Store(true)
	got := <-done
	if got.err != nil {
		t.Fatalf("bundle did not recover within its context deadline: %v", got.err)
	}
	if got.bundle.ID != ref.ID || got.bundle.Content != "skill instructions" {
		t.Fatalf("unexpected recovered bundle: %+v", got.bundle)
	}
	if !got.stats.ResponseStarted || got.stats.BytesRead != int64(len(response)) {
		t.Fatalf("unexpected transfer stats after retry: %+v", got.stats)
	}
	if count := requests.Load(); count != 3 {
		t.Fatalf("expected warmup, interrupted download and one retry; got %d requests", count)
	}
	select {
	case <-connections:
	default:
		t.Fatal("bundle recovery did not open a new connection")
	}
}

func TestResolveSkillBundleHTTP2SlowDownloadUsesContextDeadline(t *testing.T) {
	for _, budget := range []time.Duration{3 * time.Second, 100 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			var requests atomic.Int32
			client, connections := newHTTP2TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.WriteString(w, `{"bundles":[`)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(500 * time.Millisecond):
					_, _ = io.WriteString(w, `{"id":"skill"}]}`)
				case <-r.Context().Done():
				}
			}))
			client.client.Timeout = 50 * time.Millisecond
			if client.bundleClient.Timeout != 0 {
				t.Fatal("bundle downloads must use the caller's deadline")
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			bundle, stats, err := client.ResolveSkillBundle(ctx, "runtime", "task", SkillRefData{ID: "skill"})
			if budget > 500*time.Millisecond {
				if err != nil || bundle.ID != "skill" {
					t.Fatalf("healthy slow download failed: bundle=%+v err=%v", bundle, err)
				}
			} else if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected caller deadline, got %v", err)
			}
			if !stats.ResponseStarted || stats.BytesRead == 0 {
				t.Fatalf("missing body progress: %+v", stats)
			}
			if count := requests.Load(); count != 1 {
				t.Fatalf("unexpected retry during slow download or after deadline: %d", count)
			}
			<-connections
			select {
			case <-connections:
				t.Fatal("healthy connection was replaced during slow download")
			default:
			}
		})
	}
}
