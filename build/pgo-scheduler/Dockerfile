FROM golang:1.15 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY . .

RUN GOBIN="$(pwd -P)/hack/tools" ./hack/update-codegen.sh

ARG GIT_COMMIT
ARG GIT_BRANCH
ARG GO_LDFLAGS
ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_ENABLED=0

RUN mkdir -p build/_output/bin \
	&& CGO_ENABLED=$CGO_ENABLED GOOS=$GOOS GOARCH=$GOARCH GO_LDFLAGS=$GO_LDFLAGS \
	   go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH" \
		   -o build/_output/bin/pgo-scheduler \
			   ./cmd/pgo-scheduler \
	&& cp -r build/_output/bin/pgo-scheduler /usr/local/bin/pgo-scheduler

RUN ./bin/license_aggregator.sh; \
	cp -r ./licenses /licenses

FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL name="Percona Postgres Operator task scheduler" \
	  vendor="Percona" \
	  summary="Cron job processor for Percona Postgres Operator" \
	  description="Scheduler for database repeatable tasks like backups or other periodic activities." \
	  maintainer="Percona Development <info@percona.com>"

COPY redhat/licenses /licenses
COPY redhat/atomic/help.1 /help.1
COPY redhat/atomic/help.md /help.md

RUN set -ex; \
	mkdir -p /opt/cpm/bin /opt/cpm/conf /pgo-config /configs; \
	chown -R 2:2 /opt/cpm /pgo-config /configs; \
	microdnf -y install \
		gettext \
		hostname  \
		nss_wrapper \
		findutils \
		procps-ng; \
	microdnf -y clean all

COPY --from=go_builder /usr/local/bin/pgo-scheduler /opt/cpm/bin
COPY --from=go_builder /licenses /licenses
COPY bin/pgo-scheduler /opt/cpm/bin
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml

USER 2

CMD ["/opt/cpm/bin/start.sh"]
