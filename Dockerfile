FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH

RUN go build -trimpath -ldflags "-s -w" -o /out/reminders ./cmd/reminders

FROM alpine:3.21

WORKDIR /app

COPY --from=build /out/reminders /app/reminders

RUN addgroup -g 10001 -S app && adduser -u 10001 -S app -G app

USER 10001

ENTRYPOINT ["/app/reminders"]
