package bridge

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/enfein/mieru/v3/apis/client"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/stderror"
)

const (
	Invalid       int64 = -1
	Closed        int64 = -2
	Timeout       int64 = -3
	Network       int64 = -4
	MaxConfigSize       = 1024 * 1024
	MaxBufferSize       = 16 * 1024 * 1024
)

type Registry struct {
	mu          sync.Mutex
	next        uint64
	clients     map[uint64]*clientState
	connections map[uint64]*connectionState
	newClient   func() client.Client
	probes      map[uint64]*probeState
}

type clientState struct {
	lifecycle   sync.Mutex
	mu          sync.Mutex
	client      client.Client
	configured  bool
	started     bool
	stopped     bool
	connections map[uint64]*connectionState
	diagnostic  Diagnostic
}

type connectionState struct {
	mu           sync.Mutex
	readMu       sync.Mutex
	writeMu      sync.Mutex
	client       *clientState
	ctx          context.Context
	cancel       context.CancelFunc
	connection   net.Conn
	dialStarted  bool
	closed       bool
	pendingRead  error
	pendingWrite error
	diagnostic   Diagnostic
}

type destination struct {
	host string
	port uint16
}

func (d destination) Network() string { return "tcp" }
func (d destination) String() string  { return net.JoinHostPort(d.host, strconv.Itoa(int(d.port))) }

func NewRegistry() *Registry {
	return newRegistry(client.NewClient)
}

func newRegistry(factory func() client.Client) *Registry {
	return &Registry{clients: make(map[uint64]*clientState), connections: make(map[uint64]*connectionState), probes: make(map[uint64]*probeState), newClient: factory}
}

func (r *Registry) nextHandle() uint64 {
	if r.next == ^uint64(0) {
		return 0
	}
	r.next++
	return r.next
}

func (r *Registry) NewClient() uint64 {
	c := &clientState{client: r.newClient(), connections: make(map[uint64]*connectionState)}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextHandle()
	if id != 0 {
		r.clients[id] = c
	}
	return id
}

func (r *Registry) getClient(id uint64) *clientState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clients[id]
}

func (r *Registry) getConnection(id uint64) *connectionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connections[id]
}

func profileFromJSON(data []byte) (*appctlpb.ClientProfile, error) {
	config, err := ParseConfiguration(data)
	if err != nil {
		return nil, err
	}
	for _, p := range config.Profiles {
		if p.GetProfileName() == config.GetActiveProfile() {
			return p, nil
		}
	}
	return nil, configError("Active profile is missing")
}

func (r *Registry) Store(id uint64, data []byte) int64 {
	c := r.getClient(id)
	if c == nil {
		return Invalid
	}
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	stopped, started := c.stopped, c.started
	c.mu.Unlock()
	if stopped {
		return Closed
	}
	if started {
		return Invalid
	}
	c.mu.Lock()
	c.configured = false
	c.mu.Unlock()
	profile, err := profileFromJSON(data)
	if err != nil {
		c.setDiagnostic(diagnose("configuration", err))
		return Invalid
	}
	if err = c.client.Store(&client.ClientConfig{Profile: profile, Resolver: net.DefaultResolver}); err != nil {
		c.setDiagnostic(diagnose("configuration", err))
		return Invalid
	}
	c.mu.Lock()
	c.configured = true
	c.diagnostic = Diagnostic{}
	c.mu.Unlock()
	return 0
}

func (r *Registry) Start(id uint64) int64 {
	c := r.getClient(id)
	if c == nil {
		return Invalid
	}
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	stopped, configured, started := c.stopped, c.configured, c.started
	c.mu.Unlock()
	if stopped {
		return Closed
	}
	if !configured || started {
		return Invalid
	}
	if err := c.client.Start(); err != nil {
		c.setDiagnostic(diagnose("client_start", err))
		c.mu.Lock()
		c.stopped = true
		c.mu.Unlock()
		c.client.Stop()
		return Network
	}
	c.mu.Lock()
	c.started = true
	c.diagnostic = Diagnostic{}
	c.mu.Unlock()
	return 0
}

func (r *Registry) stop(c *clientState) {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	connections := c.connections
	c.connections = make(map[uint64]*connectionState)
	c.mu.Unlock()
	r.mu.Lock()
	for id := range connections {
		delete(r.connections, id)
	}
	r.mu.Unlock()
	for _, connection := range connections {
		connection.close()
	}
	c.client.Stop()
}

func (r *Registry) Stop(id uint64) {
	if c := r.getClient(id); c != nil {
		r.stop(c)
	}
}

func (r *Registry) Free(id uint64) {
	r.mu.Lock()
	c := r.clients[id]
	delete(r.clients, id)
	r.mu.Unlock()
	if c != nil {
		r.stop(c)
	}
}

func (r *Registry) NewConnection(clientID uint64) uint64 {
	c := r.getClient(clientID)
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || !c.started {
		return 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	connection := &connectionState{client: c, ctx: ctx, cancel: cancel}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextHandle()
	if id == 0 {
		cancel()
		return 0
	}
	r.connections[id] = connection
	c.connections[id] = connection
	return id
}

func validHost(host string) bool {
	return len(host) > 0 && len(host) <= 255 && !strings.ContainsAny(host, "\x00\r\n\t /[]")
}

func timeoutDuration(milliseconds int64) (time.Duration, bool) {
	if milliseconds < 0 || milliseconds > int64((24*time.Hour)/time.Millisecond) {
		return 0, false
	}
	return time.Duration(milliseconds) * time.Millisecond, true
}

func (r *Registry) Dial(id uint64, host string, port uint16, milliseconds int64) int64 {
	timeout, valid := timeoutDuration(milliseconds)
	if id == 0 || !valid || timeout == 0 || port == 0 || !validHost(host) {
		return Invalid
	}
	c := r.getConnection(id)
	if c == nil {
		return Closed
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Closed
	}
	if c.dialStarted {
		c.mu.Unlock()
		return Invalid
	}
	c.dialStarted = true
	ctx, cancel := context.WithTimeout(c.ctx, timeout)
	c.mu.Unlock()
	defer cancel()
	connection, err := c.client.client.DialContext(ctx, destination{host: host, port: port})
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || ctx.Err() != nil {
		if connection != nil {
			interruptConnection(connection)
		}
		if c.closed {
			return Closed
		}
		c.diagnostic = diagnose("target_dial", ctx.Err())
		return errorCode(ctx.Err())
	}
	if err != nil || connection == nil {
		c.diagnostic = diagnose("target_dial", err)
		if connection != nil {
			interruptConnection(connection)
		}
		return errorCode(err)
	}
	c.connection = connection
	return 0
}

func (c *connectionState) getOpenConnection() net.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	return c.connection
}

func deadline(milliseconds int64) (time.Time, bool) {
	duration, valid := timeoutDuration(milliseconds)
	if duration == 0 {
		return time.Time{}, valid
	}
	return time.Now().Add(duration), valid
}

func interruptConnection(connection net.Conn) {
	connection.Close()
}

func watchDeadline(connection net.Conn, limit time.Time) func() bool {
	if limit.IsZero() {
		return func() bool { return false }
	}
	var expired atomic.Bool
	finished := make(chan struct{})
	timer := time.AfterFunc(time.Until(limit), func() {
		expired.Store(true)
		interruptConnection(connection)
		close(finished)
	})
	return func() bool {
		if !timer.Stop() {
			<-finished
		}
		return expired.Load()
	}
}

func (r *Registry) Read(id uint64, buffer []byte, milliseconds int64) int64 {
	limit, valid := deadline(milliseconds)
	if id == 0 || !valid || len(buffer) == 0 || len(buffer) > MaxBufferSize {
		return Invalid
	}
	c := r.getConnection(id)
	if c == nil {
		return Closed
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	connection := c.getOpenConnection()
	if connection == nil {
		return Closed
	}
	if c.pendingRead != nil {
		if errors.Is(c.pendingRead, io.EOF) {
			return 0
		}
		err := c.pendingRead
		c.pendingRead = nil
		return errorCode(err)
	}
	if err := connection.SetReadDeadline(limit); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		return errorCode(err)
	}
	finish := watchDeadline(connection, limit)
	n, err := connection.Read(buffer)
	if finish() {
		err = context.DeadlineExceeded
	}
	if n > 0 {
		c.pendingRead = err
		return int64(n)
	}
	if errors.Is(err, io.EOF) {
		c.pendingRead = io.EOF
		return 0
	}
	return errorCode(err)
}

func (r *Registry) Write(id uint64, buffer []byte, milliseconds int64) int64 {
	limit, valid := deadline(milliseconds)
	if id == 0 || !valid || len(buffer) == 0 || len(buffer) > MaxBufferSize {
		return Invalid
	}
	c := r.getConnection(id)
	if c == nil {
		return Closed
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	connection := c.getOpenConnection()
	if connection == nil {
		return Closed
	}
	if c.pendingWrite != nil {
		err := c.pendingWrite
		c.pendingWrite = nil
		return errorCode(err)
	}
	if err := connection.SetWriteDeadline(limit); err != nil {
		return errorCode(err)
	}
	finish := watchDeadline(connection, limit)
	n, err := connection.Write(buffer)
	if finish() {
		err = context.DeadlineExceeded
	}
	if n > 0 {
		c.pendingWrite = err
		return int64(n)
	}
	return errorCode(err)
}

func (c *connectionState) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.cancel()
	connection := c.connection
	c.connection = nil
	c.mu.Unlock()
	if connection != nil {
		interruptConnection(connection)
	}
}

func (r *Registry) Close(id uint64) {
	r.mu.Lock()
	c := r.connections[id]
	delete(r.connections, id)
	r.mu.Unlock()
	if c == nil {
		return
	}
	c.client.mu.Lock()
	delete(c.client.connections, id)
	c.client.mu.Unlock()
	c.close()
}

func errorCode(err error) int64 {
	if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
		return Closed
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, stderror.ErrTimeout) {
		return Timeout
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return Timeout
	}
	return Network
}
