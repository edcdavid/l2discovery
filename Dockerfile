FROM registry.access.redhat.com/ubi9/ubi@sha256:b00d5990a00937bd1ef7f44547af6c7fd36e3fd410e2c89b5d2dfc1aff69fe99
RUN dnf -y --disableplugin=subscription-manager install iputils iproute ethtool pciutils
RUN dnf -y --disableplugin=subscription-manager remove python3-setuptools
COPY l2discovery-linux-amd64 /usr/bin
COPY l2discovery-linux-arm64 /usr/bin
RUN \
	if [ "$(uname -m)" = x86_64 ]; then \
		echo "Detected x86_64 CPU architecture."; \
        mv /usr/bin/l2discovery-linux-amd64 /usr/bin/l2discovery; \
	elif [ "$(uname -m)" = aarch64 ]; then \
		echo "Detected aarch64 CPU architecture."; \
        mv /usr/bin/l2discovery-linux-arm64 /usr/bin/l2discovery; \
	else \
		echo "CPU architecture is not supported." && exit 1; \
	fi
USER 0
CMD ["/bin/sh", "-c", "/usr/bin/l2discovery"]
