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

package main

import (
	"os"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovsdpdkwebhook "github.com/amorenoz/dra-driver-ovsdpdk/pkg/webhook"
)

func main() {
	var (
		certDir             string
		webhookPort         int
		healthProbeBindAddr string
	)

	pflag.StringVar(&certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"Directory containing TLS certificate and key (tls.crt, tls.key)")
	pflag.IntVar(&webhookPort, "webhook-port", 9443, "Port the webhook server listens on")
	pflag.StringVar(&healthProbeBindAddr, "health-probe-bind-address", ":8081", "Address for health probes")
	pflag.Parse()

	klog.InitFlags(nil)

	logger := klog.NewKlogr()
	ctrl.SetLogger(logger)

	cfg, err := ctrl.GetConfig()
	if err != nil {
		klog.ErrorS(err, "failed to get kubeconfig")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: healthProbeBindAddr,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			CertDir: certDir,
		}),
		LeaderElection: false,
	})
	if err != nil {
		klog.ErrorS(err, "failed to create manager")
		os.Exit(1)
	}

	decoder := admission.NewDecoder(mgr.GetScheme())
	injector, err := ovsdpdkwebhook.NewPodInjector(cfg, decoder)
	if err != nil {
		klog.ErrorS(err, "failed to create pod injector")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register("/mutate", &admission.Webhook{Handler: injector})

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "failed to add healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "failed to add readyz check")
		os.Exit(1)
	}

	klog.Info("starting webhook server")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "webhook server exited with error")
		os.Exit(1)
	}
}
