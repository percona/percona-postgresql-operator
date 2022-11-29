FROM golang:1.19 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY . .
RUN go mod download

ARG GIT_COMMIT
ARG GIT_BRANCH
ARG GO_LDFLAGS
ARG GOOS=linux
ARG GOARCH=amd64
ARG CGO_ENABLED=0

RUN mkdir -p build/_output/bin \
    && CGO_ENABLED=$CGO_ENABLED GOOS=$GOOS GOARCH=$GOARCH GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH" \
           -o build/_output/bin/postgres-operator \
               ./cmd/postgres-operator \
    && cp -r build/_output/bin/postgres-operator /usr/local/bin/postgres-operator


RUN ./bin/license_aggregator.sh ./cmd/...; \
	cp -r ./licenses /licenses

FROM registry.access.redhat.com/ubi9/ubi-minimal AS ubi9

LABEL name="Percona Postgres Operator" \
      vendor="Percona" \
      summary="Percona Postgres Operator performs Postgres database creation, adjustment and maintenance inside Kubernetes cluster" \
      description="Percona Postgres Operator simplifies and automates all essential actions for stable and continuous database operation during the whole database lifecycle" \
      maintainer="Percona Development <info@percona.com>"

RUN microdnf update -y && microdnf clean all -y

COPY licenses /licenses

COPY --from=go_builder /usr/local/bin/postgres-operator /usr/local/bin
COPY --from=go_builder /licenses /licenses

USER 2

CMD ["postgres-operator"]
