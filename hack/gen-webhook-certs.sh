#!/usr/bin/env bash
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
#
# gen-webhook-certs.sh — generate a self-signed TLS certificate for the
# ovsdpdk webhook service and store it in the ovsdpdk-webhook-certs Secret.
# Also patches the MutatingWebhookConfiguration with the CA bundle.
#
# Usage (run after "make deploy-webhook" has applied RBAC and Service):
#   hack/gen-webhook-certs.sh
#
# Requires: openssl, kubectl

set -euo pipefail

NAMESPACE="${NAMESPACE:-dra-driver-ovsdpdk}"
SERVICE="${SERVICE:-ovsdpdk-webhook}"
SECRET="${SECRET:-ovsdpdk-webhook-certs}"
WEBHOOK_CFG="${WEBHOOK_CFG:-ovsdpdk-webhook}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "Generating self-signed certificate for ${SERVICE}.${NAMESPACE}.svc ..."

# Generate CA key and cert (self-signed, also used as the server cert).
openssl req -x509 -newkey ec \
    -pkeyopt ec_paramgen_curve:P-256 \
    -days 3650 -nodes \
    -keyout "${TMPDIR}/tls.key" \
    -out    "${TMPDIR}/tls.crt" \
    -subj   "/CN=${SERVICE}.${NAMESPACE}.svc" \
    -addext "subjectAltName=DNS:${SERVICE},DNS:${SERVICE}.${NAMESPACE},DNS:${SERVICE}.${NAMESPACE}.svc,DNS:${SERVICE}.${NAMESPACE}.svc.cluster.local"

echo "Creating/updating Secret ${SECRET} in namespace ${NAMESPACE} ..."
kubectl create secret tls "${SECRET}" \
    --namespace "${NAMESPACE}" \
    --cert "${TMPDIR}/tls.crt" \
    --key  "${TMPDIR}/tls.key" \
    --dry-run=client -o yaml | kubectl apply -f -

CA_BUNDLE="$(base64 -w0 < "${TMPDIR}/tls.crt")"

echo "Patching MutatingWebhookConfiguration ${WEBHOOK_CFG} with CA bundle ..."
kubectl patch mutatingwebhookconfiguration "${WEBHOOK_CFG}" \
    --type json \
    -p "[{\"op\":\"add\",\"path\":\"/webhooks/0/clientConfig/caBundle\",\"value\":\"${CA_BUNDLE}\"}]"

echo "Verifying caBundle is set ..."
ACTUAL="$(kubectl get mutatingwebhookconfiguration "${WEBHOOK_CFG}" \
    -o jsonpath='{.webhooks[0].clientConfig.caBundle}')"
if [ -z "${ACTUAL}" ]; then
    echo "ERROR: caBundle is empty after patch!" >&2
    exit 1
fi
echo "caBundle set (${#ACTUAL} chars). Done."
