all: clean dep-tools deps test build travis-docker-push

deps:
	go mod download
	( \
		cd web/img-mngr/ && npm install \
	)

dep-tools:
	npm install aurelia-cli -g

build-web:
	mkdir -p ./dist/web/img-mngr/
	cd web/img-mngr/ && au build --env prod
	cp ./web/img-mngr/index.html ./dist/web/img-mngr/
	cp -r ./web/img-mngr/scripts/ ./dist/web/img-mngr/
	cp -r ./web/img-mngr/bootstrap/ ./dist/web/img-mngr/

build: build-web
	go build -mod=readonly
	cp ./fs-image-manager ./dist/
	@echo "Version: $(shell git log --pretty=format:'%h' -n 1)"
	(cd dist && tar cjvf fs-image-manager-$(shell git log --pretty=format:'%h' -n 1).tar.bz2 ./fs-image-manager ./web)

test:
	go test -mod=readonly ./...
	( \
		cd web/img-mngr/ && au test \
	)

install: build-web
	go install -mod=readonly

setup-docker:
	cp ./image-manager.cfg.template ./dist/image-manager.cfg

clean:
	-rm -vrf ./dist/
	-rm -v fs-image-manager

# This target is for docker dev env
setup-docker-dev:
	(cd dist && mv web webx && ln -s ../web/ .)

# This target is for Travis CI use only
travis-docker-push:
ifeq ($(shell which docker-helper),)
	sudo pip install "https://s3.amazonaws.com/install.newscred.com/docker-tools/nc-docker-tools-0.2.dev0.tar.gz"
else
	@echo "Found Docker Helper"
endif
ifdef DOCKER_USER
	docker login -u $(DOCKER_USER) -p $(DOCKER_PASS)
endif
ifeq ($(TRAVIS_BRANCH), master)
	@echo "Master docker push"
	docker-helper push
endif
ifneq ("$(TRAVIS_TAG)", "")
	@echo "Tag docker push"
	-ECR_DEFAULT_TAG="$(TRAVIS_TAG)" docker-helper push
endif
