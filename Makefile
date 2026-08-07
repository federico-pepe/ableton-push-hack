.PHONY: test vet

test:
	cd core && go test ./...

vet:
	cd core && GOOS=linux GOARCH=amd64 go vet ./...
