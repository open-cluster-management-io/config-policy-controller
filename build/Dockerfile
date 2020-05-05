FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/governance-policy-spec-sync \
    USER_UID=1001 \
    USER_NAME=governance-policy-spec-sync

# install operator binary
COPY build/_output/bin/governance-policy-spec-sync ${OPERATOR}

COPY build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
