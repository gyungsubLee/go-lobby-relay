package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/gyungsubLee/go-game-relay/internal/control"
	"github.com/gyungsubLee/go-game-relay/internal/server"
)

var errStartup = errors.New("relay: startup failed")

type requiredValue struct {
	value string
	set   bool
}

func (value *requiredValue) String() string { return "" }

func (value *requiredValue) Set(next string) error {
	if value.set {
		return errStartup
	}
	value.value = next
	value.set = true
	return nil
}

func parseConfig(args []string) (server.Config, error) {
	for _, argument := range args {
		if argument == "--" || strings.HasPrefix(argument, "-") && !strings.HasPrefix(argument, "--") {
			return server.Config{}, errStartup
		}
	}

	flags := flag.NewFlagSet("relay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var managementListen, relayNetwork, relayListen, advertisedHost, advertisedPort, operatorTokenFile requiredValue
	flags.Var(&managementListen, "management-listen", "")
	flags.Var(&relayNetwork, "relay-network", "")
	flags.Var(&relayListen, "relay-listen", "")
	flags.Var(&advertisedHost, "advertised-host", "")
	flags.Var(&advertisedPort, "advertised-port", "")
	flags.Var(&operatorTokenFile, "operator-token-file", "")
	values := []*requiredValue{
		&managementListen, &relayNetwork, &relayListen,
		&advertisedHost, &advertisedPort, &operatorTokenFile,
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return server.Config{}, errStartup
	}
	for _, value := range values {
		if !value.set || value.value == "" {
			return server.Config{}, errStartup
		}
	}
	if relayNetwork.value != "udp4" && relayNetwork.value != "udp6" || !filepath.IsAbs(operatorTokenFile.value) {
		return server.Config{}, errStartup
	}
	port, err := strconv.ParseUint(advertisedPort.value, 10, 16)
	if err != nil || port == 0 {
		return server.Config{}, errStartup
	}
	token, err := readOperatorToken(operatorTokenFile.value)
	if err != nil {
		return server.Config{}, errStartup
	}
	return server.Config{
		ManagementListen: managementListen.value,
		RelayNetwork:     relayNetwork.value,
		RelayListen:      relayListen.value,
		AdvertisedHost:   advertisedHost.value,
		AdvertisedPort:   uint16(port),
		OperatorToken:    token,
	}, nil
}

func readOperatorToken(path string) ([32]byte, error) {
	return readOperatorTokenWith(path, os.Open)
}

func readOperatorTokenWith(path string, open func(string) (*os.File, error)) ([32]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 {
		return [32]byte{}, errStartup
	}
	file, err := open(path)
	if err != nil {
		return [32]byte{}, errStartup
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 || !os.SameFile(before, opened) {
		return [32]byte{}, errStartup
	}
	after, err := os.Lstat(path)
	if err != nil || !after.Mode().IsRegular() || after.Mode().Perm() != 0o600 ||
		!os.SameFile(before, after) || !os.SameFile(opened, after) {
		return [32]byte{}, errStartup
	}
	body, err := io.ReadAll(io.LimitReader(file, 46))
	if err != nil || len(body) > 45 {
		return [32]byte{}, errStartup
	}
	switch {
	case len(body) == 43:
	case len(body) == 44 && body[43] == '\n':
	case len(body) == 45 && body[43] == '\r' && body[44] == '\n':
	default:
		return [32]byte{}, errStartup
	}
	token, err := control.ParseOperatorToken(string(body[:43]))
	if err != nil {
		return [32]byte{}, errStartup
	}
	return token, nil
}

func run(ctx context.Context, args []string) error {
	config, err := parseConfig(args)
	if err != nil {
		return errStartup
	}
	relayServer, err := server.New(config)
	if err != nil {
		return errStartup
	}
	defer relayServer.Close()
	if err := relayServer.Run(ctx); err != nil {
		return errStartup
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if run(ctx, os.Args[1:]) != nil {
		_, _ = fmt.Fprintln(os.Stderr, errStartup)
		os.Exit(1)
	}
}
