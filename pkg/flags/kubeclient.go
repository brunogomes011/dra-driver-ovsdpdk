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

	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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

// NewCoreClient builds a core Kubernetes client using the in-cluster config.
func (k *KubeClientConfig) NewCoreClient() (coreclientset.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("create in-cluster client configuration: %v", err)
	}

	cfg.QPS = float32(k.KubeAPIQPS)
	cfg.Burst = k.KubeAPIBurst

	client, err := coreclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create core client: %v", err)
	}

	return client, nil
}
