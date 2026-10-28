package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/enfein/mieru/v3/apis/client"
)

func TestProbeStagesAndCleanup(t *testing.T) {
	for _, test := range []struct {
		name, code                string
		start, dial               error
		valid, started, connected bool
	}{
		{"success", "", nil, nil, true, true, true},
		{"start", "transport_start", errors.New("SECRET_SENTINEL"), nil, true, false, false},
		{"dial", "connection_failed", nil, errors.New("SECRET_SENTINEL"), true, true, false},
		{"timeout", "timeout", nil, context.DeadlineExceeded, true, true, false},
		{"refused", "refused", nil, syscall.ECONNREFUSED, true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var peer net.Conn
			f := &fakeClient{startErr: test.start, dial: func(ctx context.Context, addr net.Addr) (net.Conn, error) {
				if addr.String() != "149.154.167.50:443" {
					t.Fatal("wrong probe destination")
				}
				if test.dial != nil {
					return nil, test.dial
				}
				var c net.Conn
				c, peer = net.Pipe()
				return c, nil
			}}
			result := probe(context.Background(), []byte(strings.Replace(testProfile, "HANDSHAKE_STANDARD", "HANDSHAKE_NO_WAIT", 1)), "149.154.167.50", 443, time.Second, func() client.Client { return f })
			if result.ConfigValid != test.valid || result.ClientStarted != test.started || result.ConnectionEstablished != test.connected {
				t.Fatal("incorrect stage result")
			}
			if f.stops != 1 || f.running {
				t.Fatal("temporary client was not stopped")
			}
			if f.config.Profile.GetHandshakeMode().String() != "HANDSHAKE_STANDARD" {
				t.Fatal("probe allowed a deferred handshake")
			}
			if peer != nil {
				defer peer.Close()
				var b [1]byte
				if _, err := peer.Read(b[:]); err != io.EOF {
					t.Fatal("probe connection was not closed")
				}
			}
			if test.code != "" && (result.Diagnostic == nil || result.Diagnostic.Code != test.code) {
				t.Fatal("incorrect diagnostic")
			}
			data, _ := json.Marshal(result)
			if strings.Contains(string(data), "SECRET_SENTINEL") || strings.Contains(string(data), "not-a-real-secret") {
				t.Fatal("probe leaked secrets")
			}
		})
	}
}

func TestProbeCancellationAndDeadline(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		entered := make(chan struct{})
		f := &fakeClient{dial: func(ctx context.Context, _ net.Addr) (net.Conn, error) {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		r := newRegistry(func() client.Client { return f })
		id := r.NewProbe()
		result := make(chan ProbeResult, 1)
		go func() { result <- r.RunProbe(id, []byte(testProfile), "127.0.0.1", 443, 50*time.Millisecond) }()
		<-entered
		if cancel {
			r.CancelProbe(id)
			r.CancelProbe(id)
		}
		select {
		case got := <-result:
			code := "timeout"
			if cancel {
				code = "cancelled"
			}
			if got.ConnectionEstablished || got.Diagnostic == nil || got.Diagnostic.Code != code {
				t.Fatal("cancel/deadline was not propagated")
			}
		case <-time.After(time.Second):
			t.Fatal("probe did not terminate")
		}
		if len(r.probes) != 0 || len(r.clients) != 0 || len(r.connections) != 0 || f.stops != 1 {
			t.Fatal("probe resources leaked")
		}
	}
}

func TestFailedStoreCannotStartPreviousConfiguration(t *testing.T) {
	f := &fakeClient{}
	r := newRegistry(func() client.Client { return f })
	id := r.NewClient()
	defer r.Free(id)
	if r.Store(id, []byte(testProfile)) != 0 {
		t.Fatal("valid config rejected")
	}
	if r.Store(id, []byte(`{"password":"SECRET_SENTINEL"}`)) != Invalid || r.Start(id) != Invalid {
		t.Fatal("invalid replacement reused old profile")
	}
	if strings.Contains(string(r.Diagnostic(id)), "SECRET_SENTINEL") {
		t.Fatal("diagnostic leaked a secret")
	}
}
