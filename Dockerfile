FROM golang:alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mailbridge ./cmd/mailbridge

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /mailbridge /mailbridge
EXPOSE 143 587
ENTRYPOINT ["/mailbridge"]
