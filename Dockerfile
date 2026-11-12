FROM golang:1.26.11-bookworm AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /out/wvc ./cmd/wvc

FROM debian:trixie-slim
ARG VERSION=dev
ARG DEBIAN_FRONTEND=noninteractive
ARG APT_MIRROR=http://deb.debian.org/debian
ARG APT_SECURITY_MIRROR=http://deb.debian.org/debian-security
ENV TMPDIR=/var/tmp
RUN sed -i \
      -e "s|http://deb.debian.org/debian-security|${APT_SECURITY_MIRROR}|g" \
      -e "s|http://deb.debian.org/debian|${APT_MIRROR}|g" \
      /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=5 update \
    && apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=5 install -y --no-install-recommends \
      ca-certificates libvirt-clients qemu-utils qemu-system-common qemu-system-x86 \
      guestfs-tools libguestfs-tools virtinst virt-v2v virt-what virt-top \
      openssh-client tzdata \
    && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
RUN groupadd --system --gid 10001 wvc \
    && useradd --uid 10001 --gid wvc --create-home --home-dir /home/wvc \
      --shell /usr/sbin/nologin wvc \
    && install -d -o wvc -g wvc -m 0700 /app/data \
    && chown -R wvc:wvc /app
WORKDIR /app
COPY --from=build /out/wvc /usr/local/bin/wvc
COPY --from=build /src/LICENSE /usr/share/doc/wvc/LICENSE
COPY --from=build /src/NOTICE /usr/share/doc/wvc/NOTICE
COPY --from=build /src/THIRD_PARTY_LICENSES.md /usr/share/doc/wvc/THIRD_PARTY_LICENSES.md
COPY --from=build /src/third_party /usr/share/doc/wvc/third-party
LABEL org.opencontainers.image.title="WebVirtCloud Go" \
      org.opencontainers.image.description="Single-binary QEMU/KVM control plane backed by libvirt and PostgreSQL" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0"
USER wvc
EXPOSE 8080
VOLUME ["/app/data"]
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=6s --start-period=3m --retries=5 \
    CMD ["/usr/local/bin/wvc", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/wvc"]
