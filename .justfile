tmp := env_var_or_default("TMPDIR", "/tmp") + "/ha-lightcraft"
goenv := "TMPDIR=" + tmp + " GOTMPDIR=" + tmp + " GOCACHE=" + tmp + "/go-cache GOTELEMETRY=off"

# Show available commands.
default: list

# Show available commands.
list:
	@just --list

# Format Go source files.
fmt:
	@mkdir -p {{tmp}}
	gofmt -w *.go

# Run unit tests.
test:
	@mkdir -p {{tmp}}
	{{goenv}} go test ./...

# Run the race detector.
race:
	@mkdir -p {{tmp}}
	{{goenv}} go test -race ./...

# Run static analysis.
vet:
	@mkdir -p {{tmp}}
	{{goenv}} go vet ./...

# Run standard golangci-lint checks.
lint:
	@mkdir -p {{tmp}}
	{{goenv}} golangci-lint run

# Build the application binary.
build:
	@mkdir -p {{tmp}}
	{{goenv}} go build -buildvcs=false -o ./ha-lightcraft .

# Run all verification gates and build.
check: test vet lint race build

# Launch the TUI against current and proposed data.
run data="data":
	@mkdir -p {{tmp}}
	{{goenv}} go run . tui --data {{data}}

# Launch the local web editor and open it in a browser.
web port="port=8080" data="data":
	@mkdir -p {{tmp}}
	{{goenv}} go run . web --data {{data}} --port {{replace(port, "port=", "")}} --open

# Pull the configured Home Assistant YAML into current and initialize proposed.
import data="data":
	@mkdir -p {{tmp}}
	{{goenv}} go run . import --data {{data}}

# Publish a draft after checking its baseline and configured references.
publish data="data":
	@mkdir -p {{tmp}}
	{{goenv}} go run . publish --data {{data}}
