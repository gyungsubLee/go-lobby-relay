package server

import (
	"context"
	"errors"
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
	rooms, err := store.New(store.Config{Limits: store.DefaultLimits()})
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
	handler, err := control.NewHandler(control.Config{
		OperatorToken:  config.OperatorToken,
		AdvertisedHost: config.AdvertisedHost,
		AdvertisedPort: advertisedPort,
		RequestRate:    control.HardManagementRequestRate,
		RequestBurst:   control.HardManagementRequestBurst,
		MaxConcurrent:  control.HardManagementConcurrent,
	}, rooms)
	if err != nil {
		return nil, errInvalidConfig
	}
	udpRelay, err := relay.New(relaySocket, rooms, relay.Config{})
	if err != nil {
		return nil, errInvalidConfig
	}

	server := &Server{
		managementListener: managementListener,
		managementServer:   control.NewServer(managementListener.Addr().String(), handler),
		relay:              udpRelay,
		rooms:              rooms,
		managementAddr:     managementListener.Addr(),
		relayAddr:          relaySocket.LocalAddr(),
		closeSignal:        make(chan struct{}),
	}
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
	name string
	err  error
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
		results <- loopResult{name: "management", err: server.managementServer.Serve(server.managementListener)}
	}()
	go func() { results <- loopResult{name: "relay", err: server.relay.Run()} }()
	go func() {
		server.rooms.RunSweeper(runContext)
		results <- loopResult{name: "sweeper"}
	}()

	received := 0
	var runErr error
	select {
	case <-ctx.Done():
	case <-server.closeSignal:
	case <-results:
		received = 1
		if !server.intentionalStop(ctx) {
			runErr = errOwnedLoop
		}
	}
	cancel()
	_ = server.shutdown()
	for received < 3 {
		<-results
		received++
	}

	server.mu.Lock()
	server.finished = true
	server.mu.Unlock()
	return runErr
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
