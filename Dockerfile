# Copyright 2026 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG GOLANG_VERSION=1.26
FROM golang:${GOLANG_VERSION} AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" \
    -o /dra-driver-ovsdpdk \
    ./cmd/dra-driver-ovsdpdk/

FROM quay.io/centos/centos:stream9 AS runtime

RUN dnf install -y util-linux-core acl && dnf clean all

COPY --from=builder /dra-driver-ovsdpdk /usr/bin/dra-driver-ovsdpdk

ENTRYPOINT ["/usr/bin/dra-driver-ovsdpdk"]
