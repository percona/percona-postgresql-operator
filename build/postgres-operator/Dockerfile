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
           -o build/_output/bin/postgres-operator \
               ./cmd/postgres-operator \
    && cp -r build/_output/bin/postgres-operator /usr/local/bin/postgres-operator

RUN ./bin/license_aggregator.sh; \
	cp -r ./licenses /licenses

FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL name="Percona Postgres Operator" \
      vendor="Percona" \
      summary="Percona Postgres Operator performs Postgres database creation, adjustment and maintenance inside Kubernetes cluster" \
      description="Percona Postgres Operator simplifies and automates all essential actions for stable and continious database operation during the whole database lifecycle" \
      maintainer="Percona Development <info@percona.com>"

COPY redhat/licenses /licenses
COPY redhat/atomic/help.1 /help.1
COPY redhat/atomic/help.md /help.md
COPY licenses /licenses

COPY --from=go_builder /usr/local/bin/postgres-operator /usr/local/bin
COPY --from=go_builder /licenses /licenses
COPY installers/ansible/roles/pgo-operator/files/pgo-backrest-repo /default-pgo-backrest-repo
COPY installers/ansible/roles/pgo-operator/files/pgo-configs /default-pgo-config
COPY conf/postgres-operator/pgo.yaml /default-pgo-config/pgo.yaml
COPY percona/templates/cluster-deployment.json /

USER 2

ENTRYPOINT ["postgres-operator"]
