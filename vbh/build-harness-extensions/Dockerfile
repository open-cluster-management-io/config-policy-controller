FROM golang:1.13.4-alpine3.10
LABEL maintainer="Red Hat <hello@redhat.com>"

LABEL "com.github.actions.name"="Build Harness"
LABEL "com.github.actions.description"="Contains CloudPosse build-harness with open-cluster-management extensions"
LABEL "com.github.actions.icon"="tool"
LABEL "com.github.actions.color"="blue"

RUN apk update && \
    apk --update add \
      bash \
      ca-certificates \
      coreutils \
      curl \
      git \
      gettext \
      make \
      py-pip && \
    git config --global advice.detachedHead false

ENV INSTALL_PATH /usr/local/bin

WORKDIR /build-harness

RUN git clone https://github.com/cloudposse/build-harness.git
RUN git clone https://github.com/open-cluster-management/build-harness-extensions.git

ENTRYPOINT ["/bin/bash"]

