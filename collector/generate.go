package main

//go:generate go run go.opentelemetry.io/collector/cmd/builder@${BUILDER_VERSION} --config ./builder-config.yml --skip-compilation
//go:generate go mod tidy
//go:generate go run ./generator/generator.go --main-path ./main.go --main-alloy-path ./main_alloy.go
