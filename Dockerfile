FROM golang:1.25-bookworm AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential libarchive-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apger ./cmd/apger \
    && go -C third_party/apgbuild build -trimpath -ldflags="-s -w" -o /out/apgbuild ./cmd/apgbuild

FROM debian:13-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        autoconf automake bash build-essential bzip2 ca-certificates cmake curl file git \
        libarchive-tools libarchive13 libtool meson ninja-build patch pkg-config \
        python3 python3-build python3-pip unzip xz-utils zstd \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 --shell /bin/bash builder

COPY --from=build /out/apger /usr/local/bin/apger
COPY --from=build /out/apgbuild /usr/local/bin/apgbuild

USER 1000:1000
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/apger"]
CMD ["serve"]
