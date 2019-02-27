FROM imyousuf/go-node-docker-img:201902-1.11.5-11.10.0
RUN mkdir -p /go/src/github.com/imyousuf/fs-image-manager/
WORKDIR /go/src/github.com/imyousuf/fs-image-manager/
ADD Makefile .
RUN make dep-tools
ADD Gopkg.lock .
ADD Gopkg.toml .
RUN mkdir -p ./web/img-mngr/
ADD ./web/img-mngr/package.json ./web/img-mngr/
ADD ./web/img-mngr/package-lock.json ./web/img-mngr/
RUN make deps
ENV CHROME_BIN=/usr/bin/chromium-browser
ENV CHROME_PATH=/usr/lib/chromium/
ADD . .
RUN make test install setup-docker
EXPOSE 8080
CMD ["fs-image-manager", "-config", "./dist/image-manager.cfg"]
