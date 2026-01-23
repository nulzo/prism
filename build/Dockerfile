ARG GO_VERSION=1.25.5

FROM golang:$GO_VERSION AS binary-build

# we need to set the working dir outside of $GOPATH
WORKDIR /home

# install deps
COPY go.mod go.sum ./

RUN go mod download && go mod verify

# copy the source code
COPY cmd ./cmd
COPY pkg ./pkg
COPY internal ./internal

# CGO_ENABLED=0 ensures static bin with no c ref or dep on c libs
# -ldflags "-s -w" strips debug info to reduce size of final build
RUN CGO_ENABLED=0 go build -o prism -ldflags "-s -w -X 'github.com/nulzo/prism/cmd.Version=$GOTENBERG_VERSION" cmd/server/main.go
