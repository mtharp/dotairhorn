FROM golang:1
ENV GO111MODULE=on
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /dotairhorn -v .

FROM debian:stable-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget ffmpeg && rm -rf /var/lib/apt/lists/* \
    && wget -O /usr/local/bin/gosu "https://github.com/tianon/gosu/releases/download/1.10/gosu-$(dpkg --print-architecture)" \
    && chmod a+rx /usr/local/bin/gosu
RUN groupadd -r dotairhorn --gid=999 && useradd -r -g dotairhorn --uid=999 --home-dir=/a dotairhorn
COPY --from=0 /dotairhorn /usr/bin/dotairhorn
COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["dotairhorn"]
