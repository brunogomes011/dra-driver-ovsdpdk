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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadclient "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// NetworkAttachmentAnnotation is the Multus pod annotation key.
	NetworkAttachmentAnnotation = nadv1.NetworkAttachmentAnnot

	// ResourceClaimTemplateAnnotation is the NAD annotation that names the
	// ResourceClaimTemplate to inject.
	ResourceClaimTemplateAnnotation = "ovsdpdk.io/resourceClaimTemplate"

	// CNIType is the CNI type field value that identifies ovsdpdk NADs.
	CNIType = "ovsdpdk"
)

// cniConfig is a minimal struct for unmarshalling the NAD spec.config field.
type cniConfig struct {
	Type string `json:"type"`
}

// claimInjection holds the resolved information for a single claim to inject.
type claimInjection struct {
	podClaimName string
	templateName string
}

// PodInjector is a controller-runtime admission.Handler that injects DRA
// ResourceClaims into pods based on their Multus network annotations.
type PodInjector struct {
	nadClient nadclient.Interface
	decoder   admission.Decoder
}

// NewPodInjector creates a PodInjector using in-cluster config.
func NewPodInjector(cfg *rest.Config, decoder admission.Decoder) (*PodInjector, error) {
	nc, err := nadclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating NAD client: %w", err)
	}
	return &PodInjector{nadClient: nc, decoder: decoder}, nil
}

// Handle implements admission.Handler.
func (p *PodInjector) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := klog.FromContext(ctx).WithValues("pod", req.Name, "namespace", req.Namespace)
	logger.Info("handling admission request")

	pod := &corev1.Pod{}
	if err := p.decoder.DecodeRaw(req.Object, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	annotation, ok := pod.Annotations[NetworkAttachmentAnnotation]
	if !ok || annotation == "" {
		logger.Info("no network annotation, skipping")
		return admission.Allowed("no network annotation")
	}
	logger.Info("found network annotation", "annotation", annotation)

	podNamespace := req.Namespace
	if podNamespace == "" {
		podNamespace = pod.Namespace
	}

	networks, err := ParsePodNetworkAnnotation(annotation, podNamespace)
	if err != nil {
		logger.Info("could not parse network annotation, skipping", "err", err)
		return admission.Allowed("unparseable network annotation")
	}
	logger.Info("parsed networks", "count", len(networks))

	injections, err := p.resolveInjections(ctx, logger, networks)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	logger.Info("resolved injections", "count", len(injections))
	if len(injections) == 0 {
		return admission.Allowed("no ovsdpdk networks")
	}

	mutatedPod := pod.DeepCopy()
	injectClaims(mutatedPod, injections)

	patched, err := json.Marshal(mutatedPod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	original, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(original, patched)
}

// resolveInjections iterates over the parsed networks and builds the list of
// claims to inject. If any confirmed ovsdpdk NAD causes an error, the whole
// admission fails.
// If present, the InterfaceRequest is used as pod-claim-name. If not,
// an auto-generated one is used.
func (p *PodInjector) resolveInjections(ctx context.Context, logger klog.Logger, networks []*nadv1.NetworkSelectionElement) ([]claimInjection, error) {
	var injections []claimInjection
	seenInterfaces := map[string]bool{}
	ovsdpdkCounter := 0

	for _, net := range networks {
		nadName := fmt.Sprintf("%s/%s", net.Namespace, net.Name)
		nad, err := p.nadClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(net.Namespace).Get(ctx, net.Name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("NAD not found, skipping", "nad", nadName)
				continue
			}
			return nil, fmt.Errorf("fetching NAD %s: %w", nadName, err)
		}

		cniType, err := extractCNIType(nad)
		if err != nil {
			logger.Info("could not parse NAD config, skipping", "nad", nadName, "err", err)
			continue
		}
		if cniType != CNIType {
			logger.Info("NAD is not ovsdpdk type, skipping", "nad", nadName, "type", cniType)
			continue
		}

		templateName, ok := nad.Annotations[ResourceClaimTemplateAnnotation]
		if !ok || templateName == "" {
			klog.Warningf("ovsdpdk NAD %s missing %s annotation", nadName, ResourceClaimTemplateAnnotation)
			continue
		}

		if errs := validation.IsDNS1123Subdomain(templateName); len(errs) > 0 {
			return nil, fmt.Errorf("NAD %s: invalid resourceClaimTemplate name %q: %s",
				nadName, templateName, strings.Join(errs, "; "))
		}

		podClaimName := net.InterfaceRequest
		if podClaimName == "" {
			podClaimName = fmt.Sprintf("ovsdpdk%d", ovsdpdkCounter)
		}
		ovsdpdkCounter++

		if seenInterfaces[podClaimName] {
			logger.Error(nil, "duplicate pod-claim-name, skipping", "podClaimName", podClaimName,
				"nad", fmt.Sprintf("%s/%s", nad.Namespace, nad.Name))
			continue
		}
		seenInterfaces[podClaimName] = true

		injections = append(injections, claimInjection{
			podClaimName: podClaimName,
			templateName: templateName,
		})
	}

	return injections, nil
}

// injectClaims mutates pod in-place, adding ResourceClaims to spec and to
// every container's resources.
func injectClaims(pod *corev1.Pod, injections []claimInjection) {
	for _, inj := range injections {
		templateName := inj.templateName
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, corev1.PodResourceClaim{
			Name:                      inj.podClaimName,
			ResourceClaimTemplateName: &templateName,
		})
	}

	claimRefs := make([]corev1.ResourceClaim, len(injections))
	for i, inj := range injections {
		claimRefs[i] = corev1.ResourceClaim{Name: inj.podClaimName}
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Resources.Claims = append(
			pod.Spec.Containers[i].Resources.Claims,
			claimRefs...,
		)
	}
}

// extractCNIType unmarshals the NAD spec.config and returns the "type" field.
func extractCNIType(nad *nadv1.NetworkAttachmentDefinition) (string, error) {
	var cfg cniConfig

	if nad.Spec.Config == "" {
		return "", nil
	}
	if err := json.Unmarshal([]byte(nad.Spec.Config), &cfg); err != nil {
		return "", err
	}
	return cfg.Type, nil
}
