.PHONY: build test lint run clean docker docker-down fmt vet

BINARY := gobalancer

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .

run: build
	./$(BINARY) -config config/config.yaml

test:
	go test ./... -v -race -count=1

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "install golangci-lint: https://golangci-lint.run/usage/install/"; exit 1; }
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .
	goimports -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

docker:
	docker-compose up --build -d

docker-down:
	docker-compose down

bench:
	go test ./... -bench=. -benchmem -run=^$ -count=3
