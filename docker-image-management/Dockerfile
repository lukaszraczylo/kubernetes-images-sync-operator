FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ARG TARGETPLATFORM
ARG TARGETARCH

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    gnupg2 \
    python3-pip \
    sudo \
    && rm -rf /var/lib/apt/lists/*

RUN echo "deb [arch=${TARGETARCH}] https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_22.04/ /" | tee /etc/apt/sources.list.d/devel:kubic:libcontainers:unstable.list \
    && curl -fsSL "https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_22.04/Release.key" | apt-key add -

RUN apt-get update && apt-get install -y --no-install-recommends \
    uidmap \
    fuse-overlayfs \
    podman \
    netavark \
    && rm -rf /var/lib/apt/lists/*

RUN adduser --disabled-password --gecos "" --uid 1001 runner \
    && groupadd docker --gid 123 \
    && usermod -aG sudo,docker runner \
    && echo "%sudo   ALL=(ALL:ALL) NOPASSWD:ALL" > /etc/sudoers \
    && echo "Defaults env_keep += \"DEBIAN_FRONTEND\"" >> /etc/sudoers

WORKDIR /home/runner

COPY storage.conf containers.conf registries.conf /home/runner/.config/containers/
COPY requirements.txt export.py cleanup.py ./
USER runner
RUN sudo chown -R runner:runner /home/runner/.config \
    && python3 -m pip install --no-cache-dir --only-binary=:all: -r requirements.txt