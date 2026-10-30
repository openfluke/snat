.PHONY: test race vet fmt check

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w *.go cmd/snat/*.go

check: race vet
	@test -z "$$(gofmt -l *.go cmd/snat/*.go)"
