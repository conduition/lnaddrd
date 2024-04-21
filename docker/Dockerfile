FROM golang:alpine as builder
COPY . /go/src/lnaddrd
WORKDIR /go/src/lnaddrd
RUN go build

FROM alpine
MAINTAINER Frédéric Logier <fredix@protonmail.com>
COPY --from=builder /go/src/lnaddrd/lnaddrd /usr/bin/
RUN mkdir /usr/share/lnaddrd
WORKDIR /usr/share/lnaddrd

RUN apk add -U --no-cache ca-certificates

EXPOSE 3441

ENTRYPOINT ["lnaddrd","/usr/share/lnaddrd/lnaddrd.yaml"]
