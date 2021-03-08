FROM golang:1.15 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY . .

RUN GOBIN="$(pwd -P)/hack/tools" ./hack/update-codegen.sh

ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_ENABLED=0

RUN mkdir -p build/_output/bin \
    && CGO_ENABLED=$CGO_ENABLED GOOS=$GOOS GOARCH=$GOARCH \
       go build \
           -o build/_output/bin/pgo-scheduler \
               ./cmd/pgo-scheduler \
    && cp -r build/_output/bin/pgo-scheduler /usr/local/bin/pgo-scheduler

FROM perconalab/percona-postgres-operator:main-pgo-base

LABEL name="pgo-scheduler" \
	summary="Perccona PostgreSQL Operator - Scheduler" \
	description="Percona PostgreSQL Operator - Scheduler"

RUN set -ex; \
    curl -Lf -o /tmp/epel-release.rpm https://dl.fedoraproject.org/pub/epel/epel-release-latest-8.noarch.rpm; \
    rpmkeys --checksig /tmp/epel-release.rpm; \
    rpm -i /tmp/epel-release.rpm; \
    rm -rf /tmp/epel-release.rpm

RUN set -ex; \
	mkdir -p /opt/cpm/bin /opt/cpm/conf /pgo-config /configs; \
	chown -R 2:2 /opt/cpm /pgo-config /configs; \
	microdnf -y install \
		gettext \
		hostname  \
		nss_wrapper \
		procps-ng; \
	microdnf -y clean all

COPY --from=go_builder /usr/local/bin/pgo-scheduler /opt/cpm/bin
COPY bin/pgo-scheduler /opt/cpm/bin
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml

USER 2

CMD ["/opt/cpm/bin/start.sh"]
