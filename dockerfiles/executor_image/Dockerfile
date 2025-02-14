FROM gcr.io/cloud-marketplace/google/debian10@sha256:88e503b14c0bfe20a3f99a630dfbfda1d5198138cdd0c32f3138a08245d9dfeb

COPY data/firecracker /usr/bin/firecracker
COPY data/jailer /usr/bin/jailer

RUN apt-get update && apt-get install -y \
  rpm curl apt-transport-https ca-certificates gnupg-agent software-properties-common fuse

RUN curl -fsSL https://download.docker.com/linux/debian/gpg | apt-key add - && \
    apt-key fingerprint 0EBFCD88 && \
    add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/debian $(lsb_release -cs) stable" && \
    apt-get update && apt-get install -y docker-ce docker-ce-cli containerd.io

# Install skopeo and umoci which we use to unpack OCI images when we're not using docker.
# Also install iproute2 ("ip" command) to configure networking on host.
RUN echo 'deb http://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/stable/Debian_10/ /' >> /etc/apt/sources.list.d/devel:kubic:libcontainers:stable.list && \
    curl -fsSL https://download.opensuse.org/repositories/devel:kubic:libcontainers:stable/Debian_10/Release.key | gpg --dearmor >> /etc/apt/trusted.gpg.d/devel_kubic_libcontainers_stable.gpg && \
    apt-get update && apt-get install -y skopeo umoci iproute2

# Configure docker credentials so we can pull marketplace.gcr.io images generated by rbe_autoconfig
RUN curl -fsSL "https://github.com/GoogleCloudPlatform/docker-credential-gcr/releases/download/v2.0.0/docker-credential-gcr_linux_amd64-2.0.0.tar.gz" | \
    tar xz --to-stdout ./docker-credential-gcr > /usr/local/bin/docker-credential-gcr && \
    chmod +x /usr/local/bin/docker-credential-gcr && \
    docker-credential-gcr configure-docker
