ARG GO_VERSION=1.25.5

FROM golang:$GO_VERSION-alpine AS binary-build

# we need to set the working dir outside of $GOPATH
WORKDIR /home

# needed for sqlite3
RUN apk add --no-cache gcc musl-dev

# install deps
COPY go.mod go.sum ./

RUN go mod download && go mod verify

# copy the source code
COPY . .

# CGO_ENABLED=0 ensures static bin with no c ref or dep on c libs
# -ldflags "-s -w" strips debug info to reduce size of final build
RUN go build -o prism -ldflags "-s -w -X 'main.Version=$PRISM_VERSION'" cmd/server/main.go

# extract binaries from build
FROM alpine:latest

# certs
RUN apk add --no-cache ca-certificates

WORKDIR /root/
COPY --from=binary-build /home/prism .
COPY --from=binary-build /home/internal/config/config.yaml ./config/config.yaml
COPY --from=binary-build /home/internal/config/models ./config/models

EXPOSE 8080

CMD ["./prism"]
