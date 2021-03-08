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
           -o build/_output/bin/apiserver \
               ./cmd/apiserver \
    && cp -r build/_output/bin/apiserver /usr/local/bin/apiserver

FROM perconalab/percona-postgres-operator:main-base

RUN microdnf update && microdnf clean all

LABEL name="Percona Postgres Operator" \
      vendor="Percona" \
      summary="Percona Postgres Operator" \
      description="Percona Postgres Operator" \
      maintainer="Percona Development <info@percona.com>"

ENV PG_MAJOR 13

RUN set -ex; \
    microdnf -y install \
	    postgresql${PG_MAJOR} \
	    hostname; \
	microdnf -y clean all

COPY --from=go_builder /usr/local/bin/apiserver /usr/local/bin
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml

USER 2

ENTRYPOINT ["/usr/local/bin/apiserver"]
