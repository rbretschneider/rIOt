package agent

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/DesyncTheThird/rIOt/internal/agent/collectors"
)

type Agent struct {
	config     *Config
	configPath string
	version    string
	registry   *collectors.Registry
	buffer     *Buffer
	client     *HTTPClient
	wsClient   *agentWSClient
}

func New(configPath, version string) (*Agent, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	registry := collectors.NewRegistry()
	registry.RegisterDefaultsWithDocker(collectors.DockerOptions{
		CollectStats: cfg.Docker.CollectStats,
		SocketPath:   cfg.Docker.SocketPath,
	})

	return &Agent{
		config:     cfg,
		configPath: configPath,
		version:    version,
		registry:   registry,
	}, nil
}

func (a *Agent) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Initialize HTTP client
	a.client = NewHTTPClient(a.config.Server.URL, a.config.Server.APIKey)

	// Initialize offline buffer
	buf, err := NewBuffer(BufferPath())
	if err != nil {
		slog.Warn("failed to init offline buffer, continuing without", "error", err)
	} else {
		a.buffer = buf
		defer a.buffer.Close()
	}

	// Register with server
	if err := a.register(ctx); err != nil {
		slog.Error("registration failed, will retry", "error", err)
	}

	// Filter collectors by config
	a.registry.FilterEnabled(a.config.Collector.Enabled)

	// Start loops
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.heartbeatLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.telemetryLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		a.updateCheckLoop(ctx)
	}()

	// Start Docker event watcher if Docker is available
	if shouldEnableDocker(a.config.Docker) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("starting Docker event watcher")
			a.dockerEventLoop(ctx)
		}()

		// Start agent WebSocket client for terminal relay if terminal is enabled
		if a.config.Docker.TerminalEnabled {
			a.wsClient = newAgentWSClient(a)
			wg.Add(1)
			go func() {
				defer wg.Done()
				slog.Info("starting agent WebSocket client for terminal relay")
				a.wsClient.run(ctx)
			}()
		}
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	<-done
	slog.Info("shutting down agent")
	cancel()
	wg.Wait()
	slog.Info("agent stopped")
	return nil
}

func (a *Agent) heartbeatLoop(ctx context.Context) {
	interval := time.Duration(a.config.Agent.HeartbeatInterval) * time.Second
	if interval == 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first heartbeat immediately
	a.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendHeartbeat(ctx)
		}
	}
}

func (a *Agent) telemetryLoop(ctx context.Context) {
	interval := time.Duration(a.config.Agent.PollInterval) * time.Second
	if interval == 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Send first telemetry immediately
	a.sendTelemetry(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.sendTelemetry(ctx)
		}
	}
}
