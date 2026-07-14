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

// Package e2e_test contains end-to-end tests for the OVS-DPDK DRA driver.
//
// Prerequisites:
//   - A Kubernetes cluster with the driver deployed (make deploy).
//   - KUBECONFIG pointing to the cluster.
//   - At least 2 worker nodes.
//
// The test suite creates its own OvsDpdkConfig and DeviceClass.  Each test
// is responsible for creating the OvsDpdkResourcePolicy objects it needs and
// cleaning them up via DeferCleanup.
package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// repoRoot is the absolute path to the repository root.
var repoRoot string

func init() {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot = filepath.Join(filepath.Dir(thisFile), "..", "..")
}

const (
	driverNamespace = "dra-driver-ovsdpdk"
	driverName      = "ovsdpdk.k8snetworkplumbingwg.io"
	testNamespace   = "default"
	hostSocketRoot  = "/var/run/ovsdpdk"
)

var (
	// cs is the typed Kubernetes clientset, initialised in BeforeSuite.
	cs kubernetes.Interface

	// kubeconfig is the path to the kubeconfig file.
	kubeconfig string

	// workers holds the names of worker nodes, discovered in BeforeSuite.
	workers []string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OVS-DPDK DRA Driver E2E Suite")
}

var _ = BeforeSuite(func() {
	kubeconfig = os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	Expect(kubeconfig).NotTo(BeEmpty(), "KUBECONFIG must be set or ~/.kube/config must exist")

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "build REST config from %s", kubeconfig)

	cs, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred(), "create clientset")

	// Discover worker nodes.
	nodeList, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "list nodes")
	for _, n := range nodeList.Items {
		if _, isCP := n.Labels["node-role.kubernetes.io/control-plane"]; !isCP {
			workers = append(workers, n.Name)
		}
	}
	Expect(len(workers)).To(BeNumerically(">=", 2), "need at least 2 worker nodes")

	// Verify the driver is running — at least 2 ResourceSlices must exist.
	Eventually(func(g Gomega) {
		slices, err := resourceSlicesForDriver(context.Background())
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(len(slices)).To(BeNumerically(">=", 2),
			"expected at least 2 ResourceSlices for driver %s", driverName)
	}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

	// Apply test-scoped cluster resources: DeviceClass and OvsDpdkConfig.
	// Per-node policies are created by individual tests via applyAndCleanup.
	applyManifest("deviceclass.yaml")
	DeferCleanup(deleteManifest, "deviceclass.yaml")

	applyManifest("ovsdpdk-config.yaml")
	DeferCleanup(deleteManifest, "ovsdpdk-config.yaml")
})

// --- Shared helpers ---

func manifestDir() string {
	return filepath.Join(repoRoot, "test", "e2e", "manifests")
}

func manifestPath(name string) string {
	return filepath.Join(manifestDir(), name)
}

// resourceSlicesForDriver returns all ResourceSlices for the OVS-DPDK driver.
func resourceSlicesForDriver(ctx context.Context) ([]resourceapi.ResourceSlice, error) {
	list, err := cs.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var result []resourceapi.ResourceSlice
	for _, s := range list.Items {
		if s.Spec.Driver == driverName {
			result = append(result, s)
		}
	}
	return result, nil
}

// resourceSlicesForNode returns ResourceSlices for a specific node.
func resourceSlicesForNode(ctx context.Context, nodeName string) ([]resourceapi.ResourceSlice, error) {
	all, err := resourceSlicesForDriver(ctx)
	if err != nil {
		return nil, err
	}
	var result []resourceapi.ResourceSlice
	for _, s := range all {
		if s.Spec.NodeName != nil && *s.Spec.NodeName == nodeName {
			result = append(result, s)
		}
	}
	return result, nil
}

// deviceNamesFromSlices extracts all device names from a list of ResourceSlices.
func deviceNamesFromSlices(sliceList []resourceapi.ResourceSlice) []string {
	var names []string
	for _, s := range sliceList {
		for _, d := range s.Spec.Devices {
			names = append(names, d.Name)
		}
	}
	return names
}
