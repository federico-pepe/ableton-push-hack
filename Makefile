.PHONY: test test-manager vet

test:
	cd core && go test ./...

test-manager:
	cd hacks/push-manager/src && go test ./semver ./update

vet:
	cd core && GOOS=linux GOARCH=amd64 go vet ./...
