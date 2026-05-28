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
	"encoding/json"
	"fmt"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ParsePodNetworkAnnotation parses the k8s.v1.cni.cncf.io/networks annotation
// from a pod.
// podNamespace is used as the default namespace for unqualified network names.
func ParsePodNetworkAnnotation(podNetworks, podNamespace string) ([]*nadv1.NetworkSelectionElement, error) {
	if podNetworks == "" {
		return nil, fmt.Errorf("parsePodNetworkAnnotation: pod annotation is empty")
	}

	// Detect JSON vs text format the same way Multus does.
	if strings.ContainsAny(podNetworks, "[{\"") {
		return parseJSONNetworkAnnotation(podNetworks, podNamespace)
	}
	return parseTextNetworkAnnotation(podNetworks, podNamespace)
}

func parseJSONNetworkAnnotation(annotation, podNamespace string) ([]*nadv1.NetworkSelectionElement, error) {
	var networks []*nadv1.NetworkSelectionElement
	if err := json.Unmarshal([]byte(annotation), &networks); err != nil {
		return nil, fmt.Errorf("parsePodNetworkAnnotation: failed to parse pod Network Annotation JSON format: %w", err)
	}
	for _, net := range networks {
		if net.Name == "" {
			return nil, fmt.Errorf("parsePodNetworkAnnotation: network name is required")
		}
		if net.Namespace == "" {
			net.Namespace = podNamespace
		}
		if err := validateDNS1123(net.Name); err != nil {
			return nil, err
		}
		if err := validateDNS1123(net.Namespace); err != nil {
			return nil, err
		}
		if net.InterfaceRequest != "" {
			if err := validateDNS1123(net.InterfaceRequest); err != nil {
				return nil, err
			}
		}
	}
	return networks, nil
}

func parseTextNetworkAnnotation(annotation, podNamespace string) ([]*nadv1.NetworkSelectionElement, error) {
	var networks []*nadv1.NetworkSelectionElement
	for _, item := range strings.Split(annotation, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		net, err := parseTextNetworkItem(item, podNamespace)
		if err != nil {
			return nil, err
		}
		networks = append(networks, net)
	}
	return networks, nil
}

// parseTextNetworkItem parses a single text-format network item of the form:
//
//	[namespace/]name[@interface]
func parseTextNetworkItem(item, podNamespace string) (*nadv1.NetworkSelectionElement, error) {
	netElem := &nadv1.NetworkSelectionElement{}

	// Split off optional @interface suffix.
	idx := strings.Index(item, "@")
	if idx >= 0 {
		netElem.InterfaceRequest = item[idx+1:]
		item = item[:idx]
		if err := validateDNS1123(netElem.InterfaceRequest); err != nil {
			return nil, err
		}
	}

	// Split optional namespace/ prefix.
	idx = strings.Index(item, "/")
	if idx >= 0 {
		netElem.Namespace = item[:idx]
		netElem.Name = item[idx+1:]
		if err := validateDNS1123(netElem.Namespace); err != nil {
			return nil, err
		}
	} else {
		netElem.Namespace = podNamespace
		netElem.Name = item
	}

	if netElem.Name == "" {
		return nil, fmt.Errorf("parsePodNetworkAnnotation: network name is required")
	}
	if err := validateDNS1123(netElem.Name); err != nil {
		return nil, err
	}

	return netElem, nil
}

// validateDNS1123 returns an error if s is not a valid DNS-1123 label.
func validateDNS1123(s string) error {
	if errs := validation.IsDNS1123Label(s); len(errs) > 0 {
		return fmt.Errorf("parsePodNetworkAnnotation: invalid value %q: %s", s, strings.Join(errs, "; "))
	}
	return nil
}
