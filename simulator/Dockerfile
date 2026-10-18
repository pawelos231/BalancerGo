FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/simulator .

FROM alpine:3.24
RUN addgroup -S -g 10001 app && adduser -S -u 10001 -G app app
WORKDIR /app
RUN chown app:app /app
COPY --from=build /out/simulator /app/simulator
USER app
EXPOSE 8080
ENTRYPOINT ["/app/simulator"]
