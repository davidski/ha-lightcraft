tmp := env_var_or_default("TMPDIR", "/tmp") + "/holiday-lighting-designer"
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

# Build a signed macOS app so Local Network permission can be granted.
app:
	@mkdir -p "{{tmp}}/Holiday Lighting Designer.app/Contents/MacOS"
	cp macos/Info.plist "{{tmp}}/Holiday Lighting Designer.app/Contents/Info.plist"
	{{goenv}} go build -buildvcs=false -o "{{tmp}}/Holiday Lighting Designer.app/Contents/MacOS/holiday-lighting-designer" .
	codesign --force --deep --sign - "{{tmp}}/Holiday Lighting Designer.app"
	@echo "{{tmp}}/Holiday Lighting Designer.app"

# Run all verification gates and build.
check: test vet race build

# Launch the TUI against a draft directory.
run draft="drafts/holiday" config="config.yaml":
	@mkdir -p {{tmp}}
	{{goenv}} go run . --draft {{draft}} --config {{config}}

# Pull the configured Home Assistant YAML into a local draft.
import draft="drafts/holiday" config="config.yaml":
	@mkdir -p {{tmp}}
	{{goenv}} go run . --draft {{draft}} --import-config {{config}}

# Publish a draft after checking its baseline and configured references.
publish draft baseline config="config.yaml":
	@mkdir -p {{tmp}}
	{{goenv}} go run . --draft {{draft}} --publish --baseline {{baseline}} --config {{config}}
