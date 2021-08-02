FROM golang:1.15 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY . .

ARG GIT_COMMIT
ARG GIT_BRANCH
ARG GO_LDFLAGS
ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_ENABLED=0

RUN mkdir -p build/_output/bin \
    && CGO_ENABLED=$CGO_ENABLED GOOS=$GOOS GOARCH=$GOARCH GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH" \
           -o build/_output/bin/apiserver \
               ./cmd/apiserver \
    && cp -r build/_output/bin/apiserver /usr/local/bin/apiserver

RUN ./bin/license_aggregator.sh; \
    cp -r ./licenses /licenses

FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL name="Percona Postgres Operator API server" \
      vendor="Percona" \
      summary="API server for Percona Postgres database management" \
      description="Percona Postgres Operator API server allows user clients to interact with Percona Postgres Operator software bundle and run databases in kubectl manner" \
      maintainer="Percona Development <info@percona.com>"

COPY redhat/licenses /licenses
COPY redhat/atomic/help.1 /help.1
COPY redhat/atomic/help.md /help.md

RUN set -ex; \
    microdnf -y install \
	    hostname; \
	microdnf -y clean all

COPY --from=go_builder /usr/local/bin/apiserver /usr/local/bin
COPY --from=go_builder /licenses /licenses
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml

USER 2

ENTRYPOINT ["/usr/local/bin/apiserver"]
