# Builder for the livedevice plugin. Ubuntu 22.04 has glibc 2.35, the same as
# AbletonOS on Push 3, so the plugin built here loads in Live on the device.
# Go is here too: the end-to-end test runs the example hack (cmd/ldexample).
FROM --platform=linux/amd64 ubuntu:22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -qq -y --no-install-recommends \
      build-essential git ca-certificates wget file ninja-build \
    && rm -rf /var/lib/apt/lists/*
ARG GO_VERSION=1.25.4
RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tgz \
    && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
# CMake from Kitware: the VST3 SDK needs 3.25+, Ubuntu 22.04 has 3.22
ARG CMAKE_VERSION=3.30.5
RUN wget -q https://github.com/Kitware/CMake/releases/download/v${CMAKE_VERSION}/cmake-${CMAKE_VERSION}-linux-x86_64.tar.gz -O /tmp/cmake.tgz \
    && tar -C /usr/local --strip-components=1 -xzf /tmp/cmake.tgz && rm /tmp/cmake.tgz
ENV PATH=/usr/local/go/bin:$PATH GOFLAGS=-buildvcs=false
