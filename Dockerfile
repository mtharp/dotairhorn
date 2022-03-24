# syntax=docker/dockerfile:1.3
FROM golang:1
WORKDIR /work
COPY . ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    go build -o /dotairhorn -ldflags "-w -s" -v -mod=readonly .

FROM debian:testing-slim
RUN rm -f /etc/apt/apt.conf.d/docker-clean; echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
RUN --mount=type=cache,target=/var/cache/apt \
    --mount=type=cache,target=/var/lib/apt \
    apt update && apt install -y --no-install-recommends ca-certificates wget ffmpeg
RUN wget -O /usr/local/bin/gosu "https://github.com/tianon/gosu/releases/download/1.14/gosu-$(dpkg --print-architecture)" \
    && chmod a+rx /usr/local/bin/gosu
RUN groupadd -r dotairhorn --gid=999 && useradd -r -m -g dotairhorn --uid=999 --home-dir=/a dotairhorn
COPY --from=0 /dotairhorn /usr/bin/dotairhorn
COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["dotairhorn"]
