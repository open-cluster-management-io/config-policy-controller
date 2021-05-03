# Copyright Contributors to the Open Cluster Management project

# Stage 1: Use image builder to build the target binaries
FROM golang:1.16 AS builder

ENV COMPONENT=config-policy-controller
ENV REPO_PATH=/go/src/github.com/open-cluster-management/${COMPONENT}
WORKDIR ${REPO_PATH}
COPY . .
RUN make build

# Stage 2: Copy the binaries from the image builder to the base image
FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV COMPONENT=config-policy-controller
ENV REPO_PATH=/go/src/github.com/open-cluster-management/${COMPONENT}
ENV OPERATOR=/usr/local/bin/${COMPONENT} \
    USER_UID=1001 \
    USER_NAME=${COMPONENT}

# install operator binary
COPY --from=builder ${REPO_PATH}/build/_output/bin/${COMPONENT} ${OPERATOR}

COPY --from=builder ${REPO_PATH}/build/bin /usr/local/bin
RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

RUN microdnf update && \
    microdnf clean all

USER ${USER_UID}
