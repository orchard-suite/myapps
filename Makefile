SHELL := /bin/bash
VERSION := $(shell cut -d' ' -f1 VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)
IMAGE := ghcr.io/orchard-suite/myapps

.PHONY: check test build docker run land

# Everything that must pass before a commit.
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: the files above need formatting" >&2; exit 1; }
	go vet ./...
	go test ./...

test:
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/myapps ./cmd/myapps

docker:
	docker build --platform linux/amd64 -t $(IMAGE):$(VERSION) .

# A local server on :8080 (pages) and :8081 (health, telemetry relay).
# Needs ORRERY_URL and ORRERY_TOKEN. The pages want the front door's headers:
#   curl -H 'X-Forwarded-Email: user@example.com' -H 'X-Forwarded-Groups: Staff' localhost:8080/v1/apps
run: build
	./dist/myapps

# Lands on main after the check passes. From a worktree:
# make land BRANCH=<branch>.
land: check
	release-damnit land $(BRANCH)
