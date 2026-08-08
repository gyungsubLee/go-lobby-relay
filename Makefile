GO_IMAGE := golang@sha256:6c5605ab3a9a9fb3c4eafe5b3d63cdbf3881caf113262b67862547b54a9db599
BUF_IMAGE := bufbuild/buf@sha256:65bd496a89c762ad7151ca9e7d885a45dacb3671a8e8ec39738b9f844d3405ea
GO := $(CURDIR)/.tools/go/bin/go
BUF := $(CURDIR)/.tools/bin/buf
GO_ENV := GOCACHE=$(CURDIR)/.cache/go-build GOMODCACHE=$(CURDIR)/.cache/go-mod

.PHONY: tools proto-generate proto-lint proto-breaking proto-baseline go-tidy go-test protocol-check csharp-compat

tools:
	./scripts/bootstrap-tools.sh

proto-generate: tools
	$(BUF) generate

proto-lint: tools
	$(BUF) lint

proto-breaking: tools
	$(BUF) breaking --against api/relay/v1/relay-v1.binpb

proto-baseline: tools
	$(BUF) build -o api/relay/v1/relay-v1.binpb

go-tidy: tools
	$(GO_ENV) $(GO) mod tidy

go-test: tools
	$(GO_ENV) $(GO) test ./...

csharp-compat:
	dotnet restore --artifacts-path $(CURDIR)/out/dotnet --locked-mode test/compat/csharp/Relay.Protocol.Compat.csproj
	dotnet run --artifacts-path $(CURDIR)/out/dotnet --no-restore --project test/compat/csharp/Relay.Protocol.Compat.csproj -- internal/protocol/testdata/v1-golden.json

protocol-check: proto-lint proto-breaking proto-generate
	git diff --exit-code -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated
	test -z "$$(git ls-files --others --exclude-standard -- api/relay/v1 gen/go/relay/v1 unity/RelaySample/Assets/Relay/Generated)"
	$(GO_ENV) $(GO) test ./internal/protocol
	$(MAKE) csharp-compat
