FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL name="Percona Postgres Operator Deployer" \
      vendor="Percona" \
      summary="Kick start image for Percona Postgres Operator essentials deployment" \
      description="Deployer app is to take care of all Percona Postgres Operator essentials deployment" \
      maintainer="Percona Development <info@percona.com>"

COPY redhat/licenses /licenses
COPY redhat/atomic/help.1 /help.1
COPY redhat/atomic/help.md /help.md
COPY licenses /licenses

ENV PG_MAJOR 13

RUN set -ex; \
    export GNUPGHOME="$(mktemp -d)"; \
    gpg --batch --keyserver keyserver.ubuntu.com --recv-keys 430BDF5C56E7C94E848EE60C1C4CBDCDCD2EFD2A 99DB70FAE1D7CE227FB6488205B555B38483C65D 94E279EB8D8F25B21810ADF121EA45AB2F86D6A1; \
    gpg --batch --export --armor 99DB70FAE1D7CE227FB6488205B555B38483C65D > ${GNUPGHOME}/RPM-GPG-KEY-centosofficial; \
    gpg --batch --export --armor 94E279EB8D8F25B21810ADF121EA45AB2F86D6A1 > ${GNUPGHOME}/RPM-GPG-KEY-EPEL-8; \
    rpmkeys --import ${GNUPGHOME}/RPM-GPG-KEY-centosofficial ${GNUPGHOME}/RPM-GPG-KEY-EPEL-8; \
    microdnf install -y findutils;

RUN set -ex; \
    microdnf -y update; \
    microdnf -y install glibc-langpack-en; \
    microdnf clean all;

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

RUN mkdir -p /opt/cpm/bin

COPY installers/ansible /ansible/postgres-operator
COPY installers/metrics/ansible /ansible/metrics
COPY installers/image/bin/pgo-deploy.sh /pgo-deploy.sh
ADD bin/common /opt/cpm/bin

ENV ANSIBLE_CONFIG="/ansible/postgres-operator/ansible.cfg"
ENV HOME="/tmp"

# Defines a unique directory name that will be utilized by the nss_wrapper in the UID script
ENV NSS_WRAPPER_SUBDIR="deployer"

ENTRYPOINT ["/opt/cpm/bin/uid_daemon.sh"]

USER 2

CMD ["/pgo-deploy.sh"]
