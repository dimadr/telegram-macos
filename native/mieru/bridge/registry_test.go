package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/apis/client"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const testProfile = `{"profileName":"test","user":{"name":"unit-user","password":"not-a-real-secret"},"servers":[{"ipAddress":"127.0.0.1","portBindings":[{"port":10001,"protocol":"UDP"},{"port":10002,"protocol":"TCP"}]}],"multiplexing":{"level":"MULTIPLEXING_HIGH"},"handshakeMode":"HANDSHAKE_STANDARD"}`

type fakeClient struct {
	mu       sync.Mutex
	config   *client.ClientConfig
	running  bool
	stops    int
	startErr error
	dial     func(context.Context, net.Addr) (net.Conn, error)
}

func (f *fakeClient) Load() (*client.ClientConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.config, nil
}

func (f *fakeClient) Store(config *client.ClientConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config = config
	return nil
}

func (f *fakeClient) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = f.startErr == nil
	return f.startErr
}

func (f *fakeClient) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	f.stops++
	return nil
}

func (f *fakeClient) IsRunning() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeClient) DialContext(ctx context.Context, addr net.Addr) (net.Conn, error) {
	return f.dial(ctx, addr)
}

func startedFake(t *testing.T, dial func(context.Context, net.Addr) (net.Conn, error)) (*Registry, uint64, *fakeClient) {
	t.Helper()
	f := &fakeClient{dial: dial}
	r := newRegistry(func() client.Client { return f })
	id := r.NewClient()
	if got := r.Store(id, []byte(testProfile)); got != 0 {
		t.Fatalf("Store = %d", got)
	}
	if got := r.Start(id); got != 0 {
		t.Fatalf("Start = %d", got)
	}
	t.Cleanup(func() { r.Free(id) })
	return r, id, f
}

func resultBeforeDeadline(t *testing.T, result <-chan int64) int64 {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not terminate")
		return Invalid
	}
}

func TestOfficialProfileLifecycle(t *testing.T) {
	r := NewRegistry()
	id := r.NewClient()
	defer r.Free(id)
	if got := r.Start(id); got != Invalid {
		t.Fatalf("unconfigured Start = %d", got)
	}
	if got := r.Store(id, []byte(`{"profiles":[`+testProfile+`],"activeProfile":"test","socks5Port":1080,"rpcPort":1081}`)); got != 0 {
		t.Fatalf("Store official config = %d", got)
	}
	stored, err := r.getClient(id).client.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Profile.GetHandshakeMode() != appctlpb.HandshakeMode_HANDSHAKE_STANDARD || stored.Profile.GetMultiplexing().GetLevel() != appctlpb.MultiplexingLevel_MULTIPLEXING_HIGH {
		t.Fatal("handshake or multiplexing configuration lost")
	}
	if len(stored.Profile.GetServers()[0].GetPortBindings()) != 2 || stored.Resolver == nil {
		t.Fatal("UDP/TCP underlay or proxy resolver configuration lost")
	}
	if got := r.Start(id); got != 0 {
		t.Fatalf("Start official client = %d", got)
	}
	if !r.getClient(id).client.IsRunning() {
		t.Fatal("official client not running")
	}
	if got := r.Start(id); got != Invalid {
		t.Fatalf("duplicate Start = %d", got)
	}
	r.Stop(id)
	if got := r.Start(id); got != Closed {
		t.Fatalf("restart stopped instance = %d", got)
	}
	if got := r.NewConnection(id); got != 0 {
		t.Fatalf("connection after stop = %d", got)
	}
	r.Stop(id)
}

func TestInvalidProfiles(t *testing.T) {
	for _, config := range []string{"", "null", "{}", "[]", `{"user":{"password":"secret"},"unknown":true}`, `{"profiles":[]}`, `{"profiles":[` + testProfile + `],"activeProfile":"missing"}`, `{"profiles":[` + testProfile + `,` + testProfile + `],"activeProfile":"test"}`} {
		r := NewRegistry()
		id := r.NewClient()
		if got := r.Store(id, []byte(config)); got != Invalid {
			t.Errorf("invalid profile returned %d", got)
		}
		r.Free(id)
	}
}

func TestInvalidZeroHandles(t *testing.T) {
	r := NewRegistry()
	if r.Dial(0, "invalid.example", 443, 1000) != Invalid || r.Read(0, make([]byte, 1), 1000) != Invalid || r.Write(0, []byte{1}, 1000) != Invalid {
		t.Fatal("zero connection handle accepted")
	}
	if r.Start(0) != Invalid || r.Store(0, []byte(testProfile)) != Invalid || r.NewConnection(0) != 0 {
		t.Fatal("zero client handle accepted")
	}
	r.Stop(0)
	r.Free(0)
	r.Close(0)
}

func TestStartedIsNotReachability(t *testing.T) {
	r, id, _ := startedFake(t, func(context.Context, net.Addr) (net.Conn, error) {
		return nil, errors.New("unreachable")
	})
	connection := r.NewConnection(id)
	defer r.Close(connection)
	if got := r.Dial(connection, "telegram.example.invalid", 443, 1000); got != Network {
		t.Fatalf("unreachable destination = %d", got)
	}
}

func TestOfficialTimeoutErrorCode(t *testing.T) {
	r, id, _ := startedFake(t, func(context.Context, net.Addr) (net.Conn, error) {
		return nil, stderror.ErrTimeout
	})
	connection := r.NewConnection(id)
	defer r.Close(connection)
	if got := r.Dial(connection, "telegram.example.invalid", 443, 1000); got != Timeout {
		t.Fatalf("official timeout error = %d", got)
	}
}

func TestCloseCancelsPendingDial(t *testing.T) {
	entered := make(chan struct{})
	r, id, _ := startedFake(t, func(ctx context.Context, addr net.Addr) (net.Conn, error) {
		if addr.Network() != "tcp" || addr.String() != "unresolved.invalid:443" {
			t.Error("destination was not passed through without local DNS")
		}
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	connection := r.NewConnection(id)
	result := make(chan int64, 1)
	go func() { result <- r.Dial(connection, "unresolved.invalid", 443, 60000) }()
	<-entered
	r.Close(connection)
	if got := resultBeforeDeadline(t, result); got != Closed {
		t.Fatalf("canceled Dial = %d", got)
	}
	if len(r.connections) != 0 || len(r.getClient(id).connections) != 0 {
		t.Fatal("closed connection still registered")
	}
}

func TestStopCancelsPendingDialAndClosesStream(t *testing.T) {
	entered := make(chan struct{})
	local, peer := net.Pipe()
	defer peer.Close()
	r, id, _ := startedFake(t, func(ctx context.Context, addr net.Addr) (net.Conn, error) {
		if addr.String() == "pending.invalid:443" {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return local, nil
	})
	stream, pending := r.NewConnection(id), r.NewConnection(id)
	if got := r.Dial(stream, "open.invalid", 443, 1000); got != 0 {
		t.Fatalf("Dial = %d", got)
	}
	readResult, dialResult := make(chan int64, 1), make(chan int64, 1)
	go func() { readResult <- r.Read(stream, make([]byte, 32), 0) }()
	go func() { dialResult <- r.Dial(pending, "pending.invalid", 443, 60000) }()
	<-entered
	r.Stop(id)
	if got := resultBeforeDeadline(t, readResult); got != Closed {
		t.Fatalf("Read after Stop = %d", got)
	}
	if got := resultBeforeDeadline(t, dialResult); got != Closed {
		t.Fatalf("Dial after Stop = %d", got)
	}
	if len(r.connections) != 0 {
		t.Fatal("stopped connections still registered")
	}
}

func TestReadWriteDeadlinesAndEOF(t *testing.T) {
	for _, operation := range []string{"read", "write", "eof"} {
		t.Run(operation, func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			r, id, _ := startedFake(t, func(context.Context, net.Addr) (net.Conn, error) { return local, nil })
			connection := r.NewConnection(id)
			defer r.Close(connection)
			if got := r.Dial(connection, "[invalid]", 443, 1000); got != Invalid {
				t.Fatalf("invalid host = %d", got)
			}
			if got := r.Dial(connection, "2001:db8::1", 443, 1000); got != 0 {
				t.Fatalf("IPv6 Dial = %d", got)
			}
			switch operation {
			case "read":
				if got := r.Read(connection, make([]byte, 8), 10); got != Timeout {
					t.Fatalf("Read timeout = %d", got)
				}
			case "write":
				if got := r.Write(connection, []byte("hello"), 10); got != Timeout {
					t.Fatalf("Write timeout = %d", got)
				}
			case "eof":
				go func() { peer.Write([]byte("data")); peer.Close() }()
				buffer := make([]byte, 8)
				if got := r.Read(connection, buffer, 1000); got != 4 || string(buffer[:4]) != "data" {
					t.Fatalf("Read = %d", got)
				}
				if got := r.Read(connection, buffer, 1000); got != 0 {
					t.Fatalf("EOF = %d", got)
				}
			}
		})
	}
}

type ignoredDeadlines struct{ net.Conn }

func (ignoredDeadlines) SetReadDeadline(time.Time) error  { return nil }
func (ignoredDeadlines) SetWriteDeadline(time.Time) error { return nil }

func TestWatchdogClosesWhenDependencyDoesNotHonorDeadline(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	r, id, _ := startedFake(t, func(context.Context, net.Addr) (net.Conn, error) { return ignoredDeadlines{local}, nil })
	connection := r.NewConnection(id)
	defer r.Close(connection)
	if got := r.Dial(connection, "destination.invalid", 443, 1000); got != 0 {
		t.Fatalf("Dial = %d", got)
	}
	result := make(chan int64, 1)
	go func() { result <- r.Read(connection, make([]byte, 1), 10) }()
	if got := resultBeforeDeadline(t, result); got != Timeout {
		t.Fatalf("watchdog timeout = %d", got)
	}
}

type shortConnection struct {
	net.Conn
	mu     sync.Mutex
	writes []byte
}

func (c *shortConnection) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(data) > 2 {
		data = data[:2]
	}
	c.writes = append(c.writes, data...)
	return len(data), nil
}

func TestPartialWriteAndConcurrentClose(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	short := &shortConnection{Conn: local}
	r, id, fake := startedFake(t, func(context.Context, net.Addr) (net.Conn, error) { return short, nil })
	connection := r.NewConnection(id)
	if got := r.Dial(connection, "destination.invalid", 443, 1000); got != 0 {
		t.Fatalf("Dial = %d", got)
	}
	payload := []byte("partial-write")
	for offset := 0; offset < len(payload); {
		count := r.Write(connection, payload[offset:], 1000)
		if count <= 0 {
			t.Fatalf("partial Write = %d", count)
		}
		offset += int(count)
	}
	if string(short.writes) != string(payload) {
		t.Fatal("partial write corrupted payload")
	}
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() { r.Close(connection); r.Stop(id); r.Free(id) })
	}
	group.Wait()
	if fake.stops != 1 || len(r.connections) != 0 || len(r.clients) != 0 {
		t.Fatal("concurrent teardown did not free handles exactly once")
	}
	if got := r.Write(connection, payload, 1000); got != Closed {
		t.Fatalf("Write after Close = %d", got)
	}
}

func TestDialDeadlineClosesLateConnection(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	r, id, _ := startedFake(t, func(ctx context.Context, _ net.Addr) (net.Conn, error) {
		<-ctx.Done()
		return local, nil
	})
	connection := r.NewConnection(id)
	defer r.Close(connection)
	if got := r.Dial(connection, "deadline.invalid", 443, 10); got != Timeout {
		t.Fatalf("late Dial = %d", got)
	}
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("late connection not closed: %v", err)
	}
}
