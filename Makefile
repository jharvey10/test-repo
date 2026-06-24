BUILDER_VERSION=v0.139.0

.PHONY: generate-otel-collector-distro

generate-otel-collector-distro:
	# Here we clear the GOOS and GOARCH env variables so we're not accidentally cross compiling the builder tool within generate
	GOOS= GOARCH= go run -C tools ./cmd sync-replaces --builder-config ../collector/builder-config.yml --go-mod ../go.mod
	cd ./collector && GOOS= GOARCH= BUILDER_VERSION=$(BUILDER_VERSION) go generate
