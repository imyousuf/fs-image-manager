all: clean dep-tools deps test build

deps:
	dep ensure -vendor-only

dep-tools:
	go get -u github.com/golang/dep/cmd/dep

build:
	mkdir -p ./dist/
	go build
	cp ./fs-image-manager ./dist/
	@echo "Version: $(shell git log --pretty=format:'%h' -n 1)"
	(cd dist && tar cjvf fs-image-manager-$(shell git log --pretty=format:'%h' -n 1).tar.bz2 ./fs-image-manager)

test:
	go test ./...

install:
	go install

clean:
	-rm -vrf ./dist/
	-rm -v fs-image-manager
