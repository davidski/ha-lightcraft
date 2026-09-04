tmp := justfile_directory() + "/.tmp"
goenv := "TMPDIR=" + tmp + " GOTMPDIR=" + tmp + " GOCACHE=" + tmp + "/go-cache GOMODCACHE=" + tmp + "/go-mod GOTELEMETRY=off"

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

# Build the application binary.
build:
	@mkdir -p {{tmp}}
	{{goenv}} go build -buildvcs=false -o {{tmp}}/holiday-lighting-designer .

# Run all verification gates and build.
check: test vet race build

# Launch the TUI against a draft directory.
run draft="drafts/holiday" config="config.yaml":
	{{goenv}} go run . --draft {{draft}} --config {{config}}

# Pull the configured Home Assistant YAML into a local draft.
import draft="drafts/holiday" config="config.yaml":
	{{goenv}} go run . --draft {{draft}} --import-config {{config}}

# Publish a draft after checking its baseline and configured references.
publish draft baseline config="config.yaml":
	{{goenv}} go run . --draft {{draft}} --publish --baseline {{baseline}} --config {{config}}
