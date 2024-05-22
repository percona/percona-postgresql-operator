FROM --platform=${BUILDPLATFORM} golang:1.22 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY go.mod go.sum ./
RUN go mod download

ARG GIT_COMMIT
ARG GIT_BRANCH
ARG BUILD_TIME
ARG GO_LDFLAGS
ARG GOOS=linux
ARG TARGETARCH
ARG CGO_ENABLED=0

COPY . .

RUN mkdir -p build/_output/bin \
   && CGO_ENABLED=$CGO_ENABLED GOARCH=${TARGETARCH} GOOS=$GOOS GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH" \
           -o build/_output/bin/postgres-operator \
               ./cmd/postgres-operator \
    && CGO_ENABLED=$CGO_ENABLED GOOS=$GOOS GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH" \
           -o build/_output/bin/extension-installer \
               ./cmd/extension-installer \
    && cp -r build/_output/bin/postgres-operator /usr/local/bin/postgres-operator \
    && cp -r build/_output/bin/extension-installer /usr/local/bin/extension-installer


RUN ./bin/license_aggregator.sh ./cmd/...; \
	cp -r ./licenses /licenses

FROM registry.access.redhat.com/ubi9/ubi-minimal AS ubi9
RUN microdnf update -y && microdnf clean all -y

LABEL name="Percona Postgres Operator" \
      vendor="Percona" \
      summary="Percona Postgres Operator performs Postgres database creation, adjustment and maintenance inside Kubernetes cluster" \
      description="Percona Postgres Operator simplifies and automates all essential actions for stable and continuous database operation during the whole database lifecycle" \
      maintainer="Percona Development <info@percona.com>"


COPY licenses /licenses

COPY --from=go_builder /usr/local/bin/postgres-operator /usr/local/bin
COPY --from=go_builder /usr/local/bin/extension-installer /usr/local/bin
COPY --from=go_builder /licenses /licenses
COPY build/postgres-operator/install-extensions.sh /usr/local/bin

USER 2

CMD ["postgres-operator"]
