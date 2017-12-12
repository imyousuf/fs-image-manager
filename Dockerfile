FROM golang:1.9.2-alpine3.6
RUN apk update && apk upgrade && \
    apk add --no-cache git g++ make
RUN go get -u github.com/golang/dep/cmd/dep
RUN mkdir -p /go/src/github.com/imyousuf/fs-image-manager/
WORKDIR /go/src/github.com/imyousuf/fs-image-manager/
ADD Gopkg.lock .
ADD Gopkg.toml .
RUN dep ensure -vendor-only
ADD . .
RUN cp ./image-manager.cfg.template ./image-manager.cfg
RUN go install
EXPOSE 8080
CMD ["fs-image-manager"]
