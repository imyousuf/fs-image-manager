FROM golang:1.9.2-alpine3.6
RUN apk update && apk upgrade && \
    apk add --no-cache git g++ make
RUN mkdir -p /go/src/github.com/imyousuf/fs-image-manager/
WORKDIR /go/src/github.com/imyousuf/fs-image-manager/
ADD Makefile .
RUN make dep-tools
ADD Gopkg.lock .
ADD Gopkg.toml .
RUN make deps
ADD . .
RUN cp ./image-manager.cfg.template ./image-manager.cfg
RUN make test install
EXPOSE 8080
CMD ["fs-image-manager"]
