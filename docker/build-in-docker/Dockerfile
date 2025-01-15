FROM golang:1.23-alpine

RUN apk update && apk add make git && mkdir /nfs-client
WORKDIR /nfs-client

COPY go.mod .
COPY go.sum .

COPY cmd/ cmd/
RUN if [ ! -d "/nfs-client/vendor" ]; then  go mod vendor; fi

RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o docker/x86_64/nfs-client-provisioner ./cmd/nfs-client-provisioner



FROM alpine:3.21
RUN apk update --no-cache && apk add ca-certificates
COPY --from=0 /nfs-client/docker/x86_64/nfs-client-provisioner /nfs-client-provisioner
ENTRYPOINT ["/nfs-client-provisioner"]