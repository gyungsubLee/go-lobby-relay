package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/gyungsubLee/go-game-relay/internal/control"
	"github.com/gyungsubLee/go-game-relay/internal/relay"
	"github.com/gyungsubLee/go-game-relay/internal/store"
)

var (
	errInvalidConfig  = errors.New("server: invalid configuration")
	errBind           = errors.New("server: listener bind failed")
	errOwnedLoop      = errors.New("server: owned loop failed")
	errAlreadyRunning = errors.New("server: already running")
	errClose          = errors.New("server: close failed")
)

type Config struct {
	ManagementListen string
	RelayNetwork     string
	RelayListen      string
	AdvertisedHost   string
	AdvertisedPort   uint16
	OperatorToken    [32]byte
}

type dependencies struct {
	listenTCP func(string, string) (net.Listener, error)
	listenUDP func(string, *net.UDPAddr) (*net.UDPConn, error)
	random    io.Reader
}

func defaultDependencies() dependencies {
	return dependencies{listenTCP: net.Listen, listenUDP: net.ListenUDP}
}

type Server struct {
	managementListener net.Listener
	managementServer   *http.Server
	relay              *relay.Relay
	rooms              *store.Store
	managementAddr     net.Addr
	relayAddr          net.Addr

	mu              sync.Mutex
	runStarted      bool
	closeRequested  bool
	finished        bool
	closeSignal     chan struct{}
	closeSignalOnce sync.Once
	fatalSignal     chan struct{}
	fatalSignalOnce sync.Once
	shutdownOnce    sync.Once
	shutdownErr     error
}

func New(config Config) (*Server, error) {
	return newWithDependencies(config, defaultDependencies())
}

func newWithDependencies(config Config, deps dependencies) (*Server, error) {
	relayAddress, err := validateConfig(config)
	if err != nil || deps.listenTCP == nil || deps.listenUDP == nil {
		return nil, errInvalidConfig
	}
	rooms, err := store.New(store.Config{Limits: store.DefaultLimits(), Random: deps.random})
	if err != nil {
		return nil, errInvalidConfig
	}
	managementListener, err := deps.listenTCP("tcp", config.ManagementListen)
	if err != nil {
		return nil, errBind
	}
	relaySocket, err := deps.listenUDP(config.RelayNetwork, relayAddress)
	if err != nil {
		_ = managementListener.Close()
		return nil, errBind
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = managementListener.Close()
			_ = relaySocket.Close()
		}
	}()

	advertisedPort := config.AdvertisedPort
	if advertisedPort == 0 {
		advertisedPort = uint16(relaySocket.LocalAddr().(*net.UDPAddr).Port)
	}
	server := &Server{
		managementListener: managementListener,
		rooms:              rooms,
		managementAddr:     managementListener.Addr(),
		relayAddr:          relaySocket.LocalAddr(),
		closeSignal:        make(chan struct{}),
		fatalSignal:        make(chan struct{}),
	}
	handler, err := control.NewHandler(control.Config{
		OperatorToken:  config.OperatorToken,
		AdvertisedHost: config.AdvertisedHost,
		AdvertisedPort: advertisedPort,
		RequestRate:    control.HardManagementRequestRate,
		RequestBurst:   control.HardManagementRequestBurst,
		MaxConcurrent:  control.HardManagementConcurrent,
		Fatal:          server.notifyFatal,
	}, rooms)
	if err != nil {
		return nil, errInvalidConfig
	}
	udpRelay, err := relay.New(relaySocket, rooms, relay.Config{})
	if err != nil {
		return nil, errInvalidConfig
	}

	server.managementServer = control.NewServer(managementListener.Addr().String(), handler)
	server.relay = udpRelay
	cleanup = false
	return server, nil
}

func validateConfig(config Config) (*net.UDPAddr, error) {
	if config.ManagementListen == "" || config.RelayListen == "" || config.AdvertisedHost == "" ||
		config.OperatorToken == ([32]byte{}) || (config.RelayNetwork != "udp4" && config.RelayNetwork != "udp6") {
		return nil, errInvalidConfig
	}
	if _, err := net.ResolveTCPAddr("tcp", config.ManagementListen); err != nil {
		return nil, errInvalidConfig
	}
	relayAddress, err := net.ResolveUDPAddr(config.RelayNetwork, config.RelayListen)
	if err != nil || relayAddress == nil || (config.AdvertisedPort == 0 && relayAddress.Port != 0) {
		return nil, errInvalidConfig
	}
	return relayAddress, nil
}

func (server *Server) ManagementAddr() net.Addr { return server.managementAddr }

func (server *Server) RelayAddr() net.Addr { return server.relayAddr }

type loopResult struct {
	name       string
	err        error
	unexpected bool
}

func (server *Server) Run(ctx context.Context) error {
	if ctx == nil {
		return errInvalidConfig
	}
	server.mu.Lock()
	if server.closeRequested {
		server.mu.Unlock()
		return nil
	}
	if server.runStarted || server.finished {
		server.mu.Unlock()
		return errAlreadyRunning
	}
	server.runStarted = true
	server.mu.Unlock()

	runContext, cancel := context.WithCancel(ctx)
	results := make(chan loopResult, 3)
	go func() {
		err := server.managementServer.Serve(server.managementListener)
		results <- server.classifyLoopResult(runContext, "management", err)
	}()
	go func() {
		err := server.relay.Run()
		results <- server.classifyLoopResult(runContext, "relay", err)
	}()
	go func() {
		server.rooms.RunSweeper(runContext)
		results <- server.classifyLoopResult(runContext, "sweeper", nil)
	}()

	runErr := coordinateLoopResults(runContext, server.closeSignal, server.fatalSignal, results, func() {
		cancel()
		_ = server.shutdown()
	})

	server.mu.Lock()
	server.finished = true
	server.mu.Unlock()
	return runErr
}

func (server *Server) notifyFatal() {
	server.fatalSignalOnce.Do(func() { close(server.fatalSignal) })
}

func (server *Server) classifyLoopResult(ctx context.Context, name string, err error) loopResult {
	return loopResult{name: name, err: err, unexpected: !server.intentionalStop(ctx)}
}

func coordinateLoopResults(
	ctx context.Context,
	closeSignal, fatalSignal <-chan struct{},
	results <-chan loopResult,
	stop func(),
) error {
	received := 0
	unexpected := false
	select {
	case <-ctx.Done():
	case <-closeSignal:
	case <-fatalSignal:
		unexpected = true
	case result := <-results:
		received = 1
		unexpected = result.unexpected
	}
	stop()
	for received < 3 {
		if (<-results).unexpected {
			unexpected = true
		}
		received++
	}
	select {
	case <-fatalSignal:
		unexpected = true
	default:
	}
	if unexpected {
		return errOwnedLoop
	}
	return nil
}

func (server *Server) intentionalStop(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.closeRequested
}

func (server *Server) Close() error {
	server.mu.Lock()
	server.closeRequested = true
	server.mu.Unlock()
	server.closeSignalOnce.Do(func() { close(server.closeSignal) })
	return server.shutdown()
}

func (server *Server) shutdown() error {
	server.shutdownOnce.Do(func() {
		managementErr := server.managementServer.Close()
		listenerErr := server.managementListener.Close()
		relayErr := server.relay.Close()
		if managementErr != nil && !errors.Is(managementErr, http.ErrServerClosed) && !errors.Is(managementErr, net.ErrClosed) ||
			listenerErr != nil && !errors.Is(listenerErr, net.ErrClosed) || relayErr != nil {
			server.shutdownErr = errClose
		}
	})
	return server.shutdownErr
}
