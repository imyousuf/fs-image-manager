all: clean dep-tools deps test build

deps:
	dep ensure -vendor-only
	( \
		cd web/img-mngr/ && npm install \
	)

dep-tools:
	go get -u github.com/golang/dep/cmd/dep
	npm install aurelia-cli -g

build-web:
	mkdir -p ./dist/web/img-mngr/
	cd web/img-mngr/ && au build --env prod
	cp ./web/img-mngr/index.html ./dist/web/img-mngr/
	cp -r ./web/img-mngr/scripts/ ./dist/web/img-mngr/

build: build-web
	go build
	cp ./fs-image-manager ./dist/
	@echo "Version: $(shell git log --pretty=format:'%h' -n 1)"
	(cd dist && tar cjvf fs-image-manager-$(shell git log --pretty=format:'%h' -n 1).tar.bz2 ./fs-image-manager)

test:
	go test ./...
	( \
		cd web/img-mngr/ && au test \
	)

install: build-web
	go install

setup-docker:
	cp ./image-manager.cfg.template ./dist/image-manager.cfg

clean:
	-rm -vrf ./dist/
	-rm -v fs-image-manager
