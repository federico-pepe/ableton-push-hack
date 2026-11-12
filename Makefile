.PHONY: test test-manager test-remote-script vet

test:
	cd core && go test ./...

test-manager:
	cd hacks/push-manager/src && go test ./semver ./update ./rscript ./plugintree ./vst3scan

test-remote-script:
	cd remote-script && python3 -m unittest discover -s tests

vet:
	cd core && GOOS=linux GOARCH=amd64 go vet ./...
