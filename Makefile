DEFAULT_TAGET: build

fmt:
	go fmt ./...
.PHONY:fmt

lint: fmt
	golint ./...
.PHONY:lint

vet: fmt
	go vet ./...
	shadow ./...
.PHONY:vet

build: vet
	CGO_ENABLED=0 GOOS=darwin go build -a -o bin/eve-marketwatch ./cmd/
.PHONY:build
