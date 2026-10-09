FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod *.go ./
RUN go test ./... && CGO_ENABLED=0 go build -o /taskqueue .
FROM alpine:3.23
RUN adduser -D app
USER app
COPY --from=build /taskqueue /usr/local/bin/taskqueue
EXPOSE 8081
CMD ["taskqueue"]
