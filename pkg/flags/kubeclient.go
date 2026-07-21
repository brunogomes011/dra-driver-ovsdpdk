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

package flags

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	coreclientset "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
)

// Scheme is the runtime scheme used by the controller-runtime manager.
var Scheme = runtime.NewScheme()

func init() { //nolint:gochecknoinits
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(ovsdpdkdrav1alpha1.AddToScheme(Scheme))
}

// KubeClientConfig holds the flags needed to build a Kubernetes client.
type KubeClientConfig struct {
	KubeAPIQPS   float64
	KubeAPIBurst int
}

// Flags returns the CLI flags for the Kubernetes client configuration.
func (k *KubeClientConfig) Flags() []cli.Flag {
	return []cli.Flag{
		&cli.Float64Flag{
			Category:    "Kubernetes client:",
			Name:        "kube-api-qps",
			Usage:       "`QPS` to use while communicating with the Kubernetes apiserver.",
			Value:       5,
			Destination: &k.KubeAPIQPS,
			EnvVars:     []string{"KUBE_API_QPS"},
		},
		&cli.IntFlag{
			Category:    "Kubernetes client:",
			Name:        "kube-api-burst",
			Usage:       "`Burst` to use while communicating with the Kubernetes apiserver.",
			Value:       10,
			Destination: &k.KubeAPIBurst,
			EnvVars:     []string{"KUBE_API_BURST"},
		},
	}
}

// RestConfig returns the in-cluster REST config with QPS/Burst applied.
func (k *KubeClientConfig) RestConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
	}
	cfg.QPS = float32(k.KubeAPIQPS)
	cfg.Burst = k.KubeAPIBurst
	return cfg, nil
}

// NewCoreClient builds a core Kubernetes client using the in-cluster config.
func (k *KubeClientConfig) NewCoreClient() (coreclientset.Interface, error) {
	cfg, err := k.RestConfig()
	if err != nil {
		return nil, err
	}

	client, err := coreclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create core client: %v", err)
	}

	return client, nil
}
