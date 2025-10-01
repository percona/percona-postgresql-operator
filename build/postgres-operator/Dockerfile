FROM --platform=${BUILDPLATFORM} golang:1.25 AS go_builder
WORKDIR /go/src/github.com/percona/percona-postgresql-operator

COPY go.mod go.sum ./
RUN go mod download

ARG GIT_COMMIT
ARG GIT_BRANCH
ARG BUILD_TIME
ARG GO_LDFLAGS
ARG GOOS=linux
ARG TARGETARCH
ARG OPERATOR_CGO_ENABLED=1
ARG EXTENSION_INSTALLER_CGO_ENABLED=1
ARG TARGETPLATFORM
ARG BUILDPLATFORM

COPY . .

RUN echo "Building on $BUILDPLATFORM, for $TARGETPLATFORM"

RUN mkdir -p build/_output/bin \
   && if [ "${TARGETPLATFORM}" = "linux/arm64" ]; then \
        apt-get update -y && apt-get install gcc-aarch64-linux-gnu -y && export CC=aarch64-linux-gnu-gcc; \
      fi \
   && if [ "${TARGETPLATFORM}" = "linux/amd64" ]; then \
        apt-get update -y && apt-get install gcc-x86-64-linux-gnu -y && export CC=x86_64-linux-gnu-gcc; \
      fi \
   && CGO_ENABLED=$OPERATOR_CGO_ENABLED GOARCH=${TARGETARCH} GOOS=$GOOS GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH -X main.BuildTime=$BUILD_TIME" \
           -o build/_output/bin/postgres-operator \
               ./cmd/postgres-operator \
    && CGO_ENABLED=$EXTENSION_INSTALLER_CGO_ENABLED GOARCH=${TARGETARCH} GOOS=$GOOS GO_LDFLAGS=$GO_LDFLAGS \
       go build -ldflags "-w -s -X main.GitCommit=$GIT_COMMIT -X main.GitBranch=$GIT_BRANCH -X main.BuildTime=$BUILD_TIME" \
           -o build/_output/bin/extension-installer \
               ./cmd/extension-installer \
    && CGO_ENABLED=$PGBACKREST_CGO_ENABLED GOARCH=${TARGETARCH} GOOS=$GOOS GO_LDFLAGS=$GO_LDFLAGS \
        go build \
            -o build/_output/bin/pgbackrest ./cmd/pgbackrest \
    && cp -r build/_output/bin/postgres-operator /usr/local/bin/postgres-operator \
    && cp -r build/_output/bin/extension-installer /usr/local/bin/extension-installer \
    && cp -r build/_output/bin/pgbackrest /usr/local/bin/pgbackrest


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
COPY --from=go_builder /usr/local/bin/pgbackrest /usr/local/bin/
COPY --from=go_builder /licenses /licenses
COPY build/postgres-operator/install-extensions.sh /usr/local/bin
COPY build/postgres-operator/relocate-extensions.sh /usr/local/bin
COPY build/postgres-operator/init-entrypoint.sh /usr/local/bin
COPY build/postgres-operator/postgres-entrypoint.sh /usr/local/bin
COPY build/postgres-operator/postgres-liveness-check.sh /usr/local/bin
COPY build/postgres-operator/postgres-readiness-check.sh /usr/local/bin
COPY hack/tools/queries /opt/crunchy/conf

RUN chgrp -R 0 /opt/crunchy/conf && chmod -R g=u opt/crunchy/conf

USER 2

CMD ["postgres-operator"]
