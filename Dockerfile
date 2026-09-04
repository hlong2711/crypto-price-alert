FROM golang:1.27-alpine

WORKDIR /app

RUN apk update
RUN apk add git

RUN go install github.com/air-verse/air@v1.61.7

# RUN addgroup -g 1000 -S app && \
  # adduser -u 1000 -S app -G app
RUN addgroup -S app && adduser -S -G app app

COPY --chown=app:app go.mod go.sum ./
RUN go mod download

COPY . .

CMD ["air", "-c", ".air.toml"]
