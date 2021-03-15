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
           -o build/_output/bin/postgres-operator \
               ./cmd/postgres-operator \
    && cp -r build/_output/bin/postgres-operator /usr/local/bin/postgres-operator

FROM perconalab/percona-postgresql-operator:main-pgo-base

LABEL name="postgres-operator" \
	summary="Percona PostgreSQL Operator" \
	description="Percona PostgreSQL Operator"

ENV PGVERSION 13
RUN set -ex; \
	microdnf -y install \
		postgresql${PGVERSION}; \
	microdnf -y clean all


COPY --from=go_builder /usr/local/bin/postgres-operator /usr/local/bin
COPY installers/ansible/roles/pgo-operator/files/pgo-backrest-repo /default-pgo-backrest-repo
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml

USER 2

ENTRYPOINT ["postgres-operator"]
