FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL name="Event dispatcher for Percona Postgres Operator" \
      vendor="Percona" \
      summary="Users entrypoint to cluster database events" \
      description="The container provides an interface for User to check lifecycle events of the database cluster" \
      maintainer="Percona Development <info@percona.com>"

COPY redhat/licenses /licenses
COPY redhat/atomic/help.1 /help.1
COPY redhat/atomic/help.md /help.md
COPY licenses /licenses

RUN set -ex; \
    microdnf -y install \
       gzip \
       tar; \
    curl -S https://s3.amazonaws.com/bitly-downloads/nsq/nsq-1.1.0.linux-amd64.go1.10.3.tar.gz | \
        tar xz --strip=2 -C /usr/local/bin/ '*/bin/*'

COPY bin/pgo-event /usr/local/bin

USER 2

ENTRYPOINT ["/usr/local/bin/pgo-event.sh"]
