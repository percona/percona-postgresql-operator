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
           -o build/_output/bin/pgo-rmdata \
               ./cmd/pgo-rmdata \
    && cp -r build/_output/bin/pgo-rmdata /usr/local/bin/pgo-rmdata

FROM perconalab/percona-postgresql-operator:main-pgo-base

LABEL name="pgo-rmdata" \
	summary="Percona PostgreSQL Operator - Remove Data" \
	description="Percona PostgreSQL Operator - Remove Data"

COPY --from=go_builder /usr/local/bin/pgo-rmdata /usr/local/bin
COPY bin/pgo-rmdata/ /usr/local/bin

USER 2

CMD ["/usr/local/bin/start.sh"]
