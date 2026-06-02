# Rover task runner. Run `just` to list recipes.

version := `tr -d '[:space:]' < VERSION 2>/dev/null || echo dev`
ldflags := "-X main.version=" + version
install_dir := env_var_or_default("ROVER_INSTALL_DIR", env_var("HOME") + "/.local/bin")

# List available recipes.
default:
    @just --list

# Build ./bin/rover.
build:
    go build -ldflags "{{ldflags}}" -o bin/rover ./cmd/rover

# Build and install rover to ~/.local/bin (override with ROVER_INSTALL_DIR).
install: build
    mkdir -p "{{install_dir}}"
    install -m 0755 bin/rover "{{install_dir}}/rover"
    @echo "Installed rover {{version}} to {{install_dir}}/rover"

# Run tests.
test:
    go test ./...

# Format Go sources.
fmt:
    gofmt -w .

# Lint (installs golangci-lint if missing).
lint:
    @which golangci-lint >/dev/null 2>&1 || curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin"
    golangci-lint run ./...

# Remove build artifacts.
clean:
    rm -rf bin/
