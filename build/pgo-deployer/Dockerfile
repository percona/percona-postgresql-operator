FROM perconalab/percona-postgres-operator:main-pgo-base

LABEL name="Percona Postgres Operator" \
      vendor="Percona" \
      summary="Percona Postgres Operator" \
      description="Percona Postgres Operator" \
      maintainer="Percona Development <info@percona.com>"

COPY installers/image/conf/kubernetes.repo /etc/yum.repos.d/kubernetes.repo

RUN set -ex; \
    microdnf -y install \
        python3

RUN set -ex; \
    curl -Lf -o /tmp/epel-release.rpm https://dl.fedoraproject.org/pub/epel/epel-release-latest-8.noarch.rpm; \
    curl -Lf -o /tmp/python3-jmespath.rpm http://mirror.centos.org/centos/8/AppStream/x86_64/os/Packages/python3-jmespath-0.9.0-11.el8.noarch.rpm; \
    rpmkeys --checksig /tmp/python3-jmespath.rpm /tmp/epel-release.rpm; \
    rpm -i /tmp/epel-release.rpm /tmp/python3-jmespath.rpm; \
    rm -rf /tmp/epel-release.rpm /tmp/python3-jmespath.rpm

RUN set -ex; \
    microdnf -y install \
        kubectl \
        ansible-2.9.* \
        which \
        gettext \
        openssl; \
    microdnf  -y clean all

COPY installers/ansible /ansible/postgres-operator
COPY installers/metrics/ansible /ansible/metrics
COPY installers/image/bin/pgo-deploy.sh /pgo-deploy.sh
COPY bin/uid_daemon.sh /uid_daemon.sh

ENV ANSIBLE_CONFIG="/ansible/postgres-operator/ansible.cfg"
ENV HOME="/tmp"

RUN chmod g=u /etc/passwd
RUN chmod g=u /uid_daemon.sh

ENTRYPOINT ["/uid_daemon.sh"]

USER 2

CMD ["/pgo-deploy.sh"]
