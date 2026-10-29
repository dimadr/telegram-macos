package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/enfein/mieru/v3/apis/client"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

type Diagnostic struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func diagnose(stage string, err error) Diagnostic {
	d := Diagnostic{stage, "connection_failed", "The Mieru/target connection failed. Authentication failure cannot be inferred from this error."}
	var config *ConfigurationError
	var dns *net.DNSError
	switch {
	case errors.As(err, &config):
		d.Code, d.Message = "configuration", config.Message
	case errors.Is(err, context.DeadlineExceeded):
		d.Code, d.Message = "timeout", "Connection timed out before the Mieru/target handshake completed."
	case errors.Is(err, context.Canceled):
		d.Code, d.Message = "cancelled", "Connection check was cancelled."
	case errors.As(err, &dns):
		d.Code, d.Message = "dns", "A server or target address could not be resolved."
	case errors.Is(err, syscall.ECONNREFUSED):
		d.Code, d.Message = "refused", "A connection was refused while establishing the Mieru/target path."
	default:
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			d.Code, d.Message = "timeout", "Connection timed out before the Mieru/target handshake completed."
		} else if stage == "configuration" {
			d.Code, d.Message = "configuration", "The official Client API rejected this profile."
		} else if stage == "client_start" {
			d.Code, d.Message = "transport_start", "The Mieru transport could not be initialized."
		}
	}
	return d
}

func (c *clientState) setDiagnostic(d Diagnostic) { c.mu.Lock(); c.diagnostic = d; c.mu.Unlock() }
func (r *Registry) Diagnostic(id uint64) []byte {
	c := r.getClient(id)
	if c == nil {
		data, _ := json.Marshal(Diagnostic{"lifecycle", "closed", "Client no longer exists."})
		return data
	}
	c.mu.Lock()
	d := c.diagnostic
	c.mu.Unlock()
	data, _ := json.Marshal(d)
	return data
}

func (r *Registry) ConnectionDiagnostic(id uint64) []byte {
	c := r.getConnection(id)
	if c == nil {
		data, _ := json.Marshal(Diagnostic{"target_dial", "closed", "Connection no longer exists."})
		return data
	}
	c.mu.Lock()
	d := c.diagnostic
	c.mu.Unlock()
	data, _ := json.Marshal(d)
	return data
}

type probeState struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *Registry) NewProbe() uint64 {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextHandle()
	if id == 0 {
		cancel()
		return 0
	}
	r.probes[id] = &probeState{ctx, cancel}
	return id
}

func (r *Registry) CancelProbe(id uint64) {
	r.mu.Lock()
	p := r.probes[id]
	delete(r.probes, id)
	r.mu.Unlock()
	if p != nil {
		p.cancel()
	}
}

func (r *Registry) RunProbe(id uint64, data []byte, host string, port uint16, timeout time.Duration) ProbeResult {
	r.mu.Lock()
	p := r.probes[id]
	r.mu.Unlock()
	if p == nil {
		d := diagnose("target_dial", context.Canceled)
		return ProbeResult{Diagnostic: &d}
	}
	defer r.CancelProbe(id)
	return probe(p.ctx, data, host, port, timeout, r.newClient)
}

type ProbeResult struct {
	ConfigValid           bool        `json:"configValid"`
	ClientStarted         bool        `json:"clientStarted"`
	ConnectionEstablished bool        `json:"connectionEstablished"`
	Diagnostic            *Diagnostic `json:"diagnostic,omitempty"`
}

func Probe(data []byte, host string, port uint16, timeout time.Duration) ProbeResult {
	return probe(context.Background(), data, host, port, timeout, client.NewClient)
}

func probe(parent context.Context, data []byte, host string, port uint16, timeout time.Duration, factory func() client.Client) ProbeResult {
	result := ProbeResult{}
	fail := func(stage string, err error) ProbeResult {
		d := diagnose(stage, err)
		result.Diagnostic = &d
		return result
	}
	if !validHost(host) || port == 0 || timeout <= 0 || timeout > 30*time.Second {
		return fail("configuration", configError("Invalid connection check target or timeout"))
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if ctx.Err() != nil {
		return fail("target_dial", ctx.Err())
	}
	p, err := profileFromJSON(data)
	if err != nil {
		return fail("configuration", err)
	}
	p = proto.Clone(p).(*pb.ClientProfile)
	p.HandshakeMode = pb.HandshakeMode_HANDSHAKE_STANDARD.Enum()
	c := factory()
	defer c.Stop()
	if err := c.Store(&client.ClientConfig{Profile: p, Resolver: net.DefaultResolver}); err != nil {
		return fail("configuration", err)
	}
	result.ConfigValid = true
	if err := c.Start(); err != nil {
		return fail("client_start", err)
	}
	result.ClientStarted = true
	conn, err := c.DialContext(ctx, destination{host, port})
	if conn != nil {
		defer conn.Close()
	}
	if err != nil {
		return fail("target_dial", err)
	}
	if ctx.Err() != nil {
		return fail("target_dial", ctx.Err())
	}
	if conn == nil {
		return fail("target_dial", errors.New("empty connection"))
	}
	result.ConnectionEstablished = true
	return result
}
