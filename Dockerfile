FROM golang:1.14 as build
LABEL Maintainer="Soichi Sumi <soichi.sumi@gmail.com>"

COPY . /go/src/tmp
WORKDIR /go/src/tmp
RUN make go-build

FROM alpine:latest
RUN apk --no-cache add ca-certificates \
    && apk add --no-cache libc6-compat
COPY --from=build /go/src/tmp/exe .
ENTRYPOINT ["./exe"]
