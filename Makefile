.PHONY: fmt-check vet test race check

GO_TEST ?= go test

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

vet:
	go vet ./...

test:
	$(GO_TEST) ./...

race:
	$(GO_TEST) -race ./...

check: fmt-check vet test race
