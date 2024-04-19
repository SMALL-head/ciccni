FROM ubuntu:20.04 as ovs-debs

ARG OVS_VERSION=2.13.8

RUN apt-get update && \
    DEBIAN_FRONTEND="noninteractive" apt-get install -y --no-install-recommends wget curl git ca-certificates build-essential fakeroot graphviz \
            bzip2 autoconf automake debhelper dh-python dh-autoreconf libssl-dev libtool openssl procps \
            python3-all python3-twisted python3-zope.interface python3-sphinx \
            libunbound-dev

COPY openvswitch-2.13.8.tar.gz .
RUN  tar xz -f openvswitch-2.13.8.tar.gz -C /tmp 
RUN cd /tmp/openvswitch* && DEB_BUILD_OPTIONS='parallel=4 nocheck' fakeroot debian/rules binary
RUN cd /tmp && mkdir ovs-debs && \
    mv libopenvswitch_*.deb openvswitch-common_*.deb openvswitch-switch_*.deb \
       openvswitch-ipsec_*.deb ovs-debs/

FROM ubuntu:20.10

LABEL description="A Docker image based on Ubuntu 18.04 which includes Open vSwitch built from source."

COPY --from=ovs-debs /tmp/ovs-debs/* /tmp/ovs-debs/

# We clean-up apt cache after installing packages to reduce the size of the
# final image
RUN apt-get update && \
    apt-get install -y --no-install-recommends iptables libstrongswan-standard-plugins && \
    (dpkg -i /tmp/ovs-debs/*.deb || apt-get -f -y --no-install-recommends install) && \
    rm -rf /var/cache/apt/* /var/lib/apt/lists/* && \
    rm -rf /tmp/ovs-debs 