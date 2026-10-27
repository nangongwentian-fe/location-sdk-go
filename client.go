package locationsdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/nangongwentian-fe/location-sdk-go/protocol"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MaxBatchSize = 100

var ErrUnavailable = errors.New("location engine unavailable")
var ErrClosed = errors.New("location engine client closed")

type Options struct {
	Path         string
	SHA256       string
	Version      string
	StartTimeout time.Duration
	CallTimeout  time.Duration
	Logger       *slog.Logger
}

type Client struct {
	metrics   counters
	options   Options
	mu        sync.RWMutex
	process   *plugin.Client
	command   *exec.Cmd
	startedAt time.Time
	rpc       protocol.EngineClient
	info      *protocol.InfoResponse
	closed    bool
	cancel    context.CancelFunc
	done      chan struct{}
}

// Open verifies the artifact and handshake before returning. Close must be called.
func Open(ctx context.Context, options Options) (*Client, error) {
	if !filepath.IsAbs(options.Path) {
		return nil, errors.New("engine path must be absolute")
	}
	digest, err := hex.DecodeString(options.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return nil, errors.New("engine SHA256 must contain 64 hexadecimal characters")
	}
	options.SHA256 = strings.ToLower(options.SHA256)
	if options.Version == "" {
		return nil, errors.New("expected engine version is required")
	}
	if options.StartTimeout == 0 {
		options.StartTimeout = 10 * time.Second
	}
	if options.CallTimeout == 0 {
		options.CallTimeout = 5 * time.Second
	}
	if options.StartTimeout < 0 || options.CallTimeout < 0 {
		return nil, errors.New("timeouts must be positive")
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	life, cancel := context.WithCancel(ctx)
	c := &Client{options: options, cancel: cancel, done: make(chan struct{})}
	if err := c.start(life); err != nil {
		cancel()
		return nil, err
	}
	go c.supervise(life)
	return c, nil
}

func (c *Client) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(c.options.Path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != c.options.SHA256 {
		return errors.New("engine checksum mismatch")
	}
	digest, _ := hex.DecodeString(c.options.SHA256)
	life, stop := context.WithCancel(ctx)
	startupTimer := time.AfterFunc(c.options.StartTimeout, stop)
	defer startupTimer.Stop()
	adopted := false
	defer func() {
		if !adopted {
			stop()
		}
	}()
	cmd := exec.CommandContext(life, c.options.Path)
	for _, name := range []string{"SystemRoot", "TEMP", "TMP", "TMPDIR"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	p := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: Handshake, Plugins: map[string]plugin.Plugin{PluginName: &Plugin{}},
		Cmd: cmd, SkipHostEnv: true, AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		StartTimeout: c.options.StartTimeout, AutoMTLS: true,
		SecureConfig: &plugin.SecureConfig{Checksum: digest, Hash: sha256.New()},
		Logger:       hclog.NewNullLogger(), Stderr: io.Discard,
	})
	connected, err := p.Client()
	if err != nil {
		forceStop(p, cmd)
		return fmt.Errorf("start engine: %w", err)
	}
	raw, err := connected.Dispense(PluginName)
	if err != nil {
		forceStop(p, cmd)
		return err
	}
	rpc, ok := raw.(protocol.EngineClient)
	if !ok {
		forceStop(p, cmd)
		return errors.New("unexpected engine client")
	}
	call, cancel := context.WithTimeout(life, c.options.CallTimeout)
	info, err := rpc.Info(call, &protocol.InfoRequest{})
	cancel()
	if err == nil {
		compatible := false
		for _, format := range info.StateFormats {
			if format == StateFormat {
				compatible = true
			}
		}
		if info.Version != c.options.Version || info.ProtocolVersion != ProtocolVersion || !compatible {
			err = errors.New("engine version, protocol or state format mismatch")
		}
	}
	if err != nil {
		forceStop(p, cmd)
		return err
	}
	c.mu.Lock()
	if c.closed || life.Err() != nil {
		c.mu.Unlock()
		forceStop(p, cmd)
		return ErrClosed
	}
	c.process, c.rpc, c.info = p, rpc, info
	c.command, c.startedAt = cmd, time.Now()
	adopted = true
	c.mu.Unlock()
	c.options.Logger.Info("location engine ready", "version", info.Version)
	return nil
}

// Kill the OS process before go-plugin cleanup: its graceful Shutdown RPC has
// no deadline and cannot be used as the timeout boundary for a frozen engine.
func forceStop(process *plugin.Client, command *exec.Cmd) {
	if command != nil && command.Process != nil {
		_ = command.Process.Kill()
	}
	if process != nil {
		process.Kill()
	}
}
func (c *Client) supervise(ctx context.Context) {
	defer close(c.done)
	defer func() {
		c.mu.Lock()
		process, command := c.process, c.command
		c.process, c.command, c.rpc, c.info = nil, nil, nil, nil
		c.mu.Unlock()
		forceStop(process, command)
	}()
	delay := time.Second
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		c.mu.Lock()
		old, command := c.process, c.command
		dead := old == nil || old.Exited()
		if dead {
			c.process, c.command, c.rpc, c.info = nil, nil, nil, nil
		} else if time.Since(c.startedAt) >= time.Minute {
			delay = time.Second
		}
		c.mu.Unlock()
		if dead {
			forceStop(old, command)
			c.options.Logger.Error("location engine unavailable", "restart_delay", delay)
			retry := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				retry.Stop()
				return
			case <-retry.C:
			}
			if err := c.start(ctx); err != nil {
				c.options.Logger.Error("location engine restart failed", "error", err)
			}
			delay = nextRestartDelay(delay)
		}
		timer.Reset(time.Second)
	}
}
func nextRestartDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	<-c.done
	return nil
}
func (c *Client) connection() (protocol.EngineClient, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil, ErrClosed
	}
	if c.rpc == nil || c.process == nil || c.process.Exited() {
		return nil, ErrUnavailable
	}
	return c.rpc, nil
}
func (c *Client) Info(ctx context.Context) (*protocol.InfoResponse, error) {
	rpc, err := c.connection()
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, c.options.CallTimeout)
	defer cancel()
	started := time.Now()
	result, err := rpc.Info(call, &protocol.InfoRequest{})
	c.recordCall(time.Since(started), err)
	return result, callError(err)
}
func callError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return &ValidationError{Message: status.Convert(err).Message()}
	case codes.Unavailable, codes.DeadlineExceeded:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return err
}
func (c *Client) ValidateConfig(ctx context.Context, input *protocol.ConfigRequest) (*protocol.ConfigResponse, error) {
	rpc, err := c.connection()
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, c.options.CallTimeout)
	defer cancel()
	started := time.Now()
	result, err := rpc.ValidateConfig(call, input)
	c.recordCall(time.Since(started), err)
	return result, callError(err)
}
func (c *Client) EvaluateLocations(ctx context.Context, input *protocol.LocationRequest) (*protocol.LocationResponse, error) {
	if input == nil || len(input.Samples) == 0 || len(input.Samples) > MaxBatchSize {
		return nil, &ValidationError{Message: "location batch must contain 1..100 samples"}
	}
	rpc, err := c.connection()
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, c.options.CallTimeout)
	defer cancel()
	started := time.Now()
	result, err := rpc.EvaluateLocations(call, input)
	c.recordCall(time.Since(started), err)
	c.options.Logger.Debug("location evaluation", "count", len(input.Samples), "duration", time.Since(started), "failed", err != nil)
	return result, callError(err)
}
func (c *Client) EvaluateAlarms(ctx context.Context, input *protocol.AlarmRequest) (*protocol.AlarmResponse, error) {
	if input == nil || len(input.Items) == 0 || len(input.Items) > MaxBatchSize {
		return nil, &ValidationError{Message: "alarm batch must contain 1..100 items"}
	}
	rpc, err := c.connection()
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, c.options.CallTimeout)
	defer cancel()
	started := time.Now()
	result, err := rpc.EvaluateAlarms(call, input)
	c.recordCall(time.Since(started), err)
	c.options.Logger.Debug("alarm evaluation", "count", len(input.Items), "duration", time.Since(started), "failed", err != nil)
	return result, callError(err)
}
