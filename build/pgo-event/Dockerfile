FROM perconalab/percona-postgres-operator:main-pgo-base

LABEL name="pgo-event" \
    summary="Percona PostgreSQL Operator - pgo-event" \
    description="Percona PostgreSQL Operator - pgo-event"

RUN set -ex; \
    microdnf -y install \
       gzip \
       tar; \
    curl -S https://s3.amazonaws.com/bitly-downloads/nsq/nsq-1.1.0.linux-amd64.go1.10.3.tar.gz | \
        tar xz --strip=2 -C /usr/local/bin/ '*/bin/*'

COPY bin/pgo-event /usr/local/bin

USER 2

ENTRYPOINT ["/usr/local/bin/pgo-event.sh"]
