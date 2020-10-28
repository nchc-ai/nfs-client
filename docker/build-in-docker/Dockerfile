FROM golang:1.15.2

# workaround for glide fetch Gitlab private repo
#RUN mkdir /root/.ssh
#COPY ./deploy/build-in-docker/ssh/id_rsa /root/.ssh
#COPY ./deploy/build-in-docker/ssh/known_hosts /root/.ssh
#RUN chmod 700 /root/.ssh/id_rsa
#RUN git config --global url."git@gitlab.com:nchc-ai/".insteadOf "https://gitlab.com/nchc-ai/"


RUN mkdir /nfs-client
WORKDIR /nfs-client

# Using go mod download to speed up Golang Docker builds
# https://medium.com/@petomalina/using-go-mod-download-to-speed-up-golang-docker-builds-707591336888?fbclid=IwAR0Jzs5PqepVJTHsshx5sKDftoCbM3IdlW-RD8_hcH6Nlets3lm3Y0LiX3U

# COPY go.mod and go.sum files to the workspace
COPY go.mod .
COPY go.sum .


COPY . .
RUN if [ ! -d "/nfs-client/vendor" ]; then  go mod vendor; fi

RUN make build



FROM alpine:3.7
RUN apk update --no-cache && apk add ca-certificates
COPY --from=0 /nfs-client/docker/x86_64/nfs-client-provisioner /nfs-client-provisioner
ENTRYPOINT ["/nfs-client-provisioner"]