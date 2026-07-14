/*
 * Copyright 2026 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- Template rendering ---

func renderManifest(name string, data any) (string, error) {
	raw, err := os.ReadFile(manifestPath(name))
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", name, err)
	}
	tmpl, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}
	return buf.String(), nil
}

func mustRenderManifest(name string, data any) string {
	s, err := renderManifest(name, data)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "render %s", name)
	return s
}

// --- Template data types ---

type policyData struct {
	Name, NodeName string
	Bridges        []string
}

// --- kubectl manifest apply/delete ---
//
// All helpers use the package-level kubeconfig var set in BeforeSuite.

// applyManifest applies a static YAML file from manifests/.
func applyManifest(name string) {
	GinkgoHelper()
	runKubectl("apply", "-f", manifestPath(name))
}

// deleteManifest deletes a static YAML file from manifests/ (ignores not-found).
func deleteManifest(name string) {
	GinkgoHelper()
	runKubectl("delete", "--ignore-not-found", "-f", manifestPath(name))
}

// applyYAML applies a rendered YAML string via kubectl.
func applyYAML(manifest string) {
	GinkgoHelper()
	runKubectlStdin(manifest, "apply", "-f", "-")
}

// deleteYAML deletes resources described by a rendered YAML string and waits
// for them to be fully removed (finalizers included).  The 30s timeout ensures
// we detect stuck unprepare rather than hanging indefinitely.
func deleteYAML(manifest string) {
	GinkgoHelper()
	runKubectlStdin(manifest, "delete", "--ignore-not-found", "--timeout=30s", "-f", "-")
}

// applyAndCleanup applies a rendered YAML string and registers DeferCleanup
// to delete it when the current test/suite scope exits.
func applyAndCleanup(manifest string) {
	GinkgoHelper()
	applyYAML(manifest)
	DeferCleanup(deleteYAML, manifest)
}

func runKubectl(args ...string) {
	GinkgoHelper()
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	out, err := exec.Command("kubectl", allArgs...).CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl %v:\n%s", args, out)
}

func runKubectlStdin(stdin string, args ...string) {
	GinkgoHelper()
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.Command("kubectl", allArgs...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "kubectl %v:\n%s", args, out)
}

// waitForDeviceInSlice polls until the named device appears in the
// ResourceSlices for the given node.
func waitForDeviceInSlice(ctx context.Context, nodeName, deviceName string) {
	GinkgoHelper()
	EventuallyWithOffset(1, func(g Gomega) {
		nodeSlices, err := resourceSlicesForNode(ctx, nodeName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(deviceNamesFromSlices(nodeSlices)).To(ContainElement(deviceName))
	}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
}
