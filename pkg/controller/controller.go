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

package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/devicestate"
)

const (
	resourcePolicySyncEventName = "resource-policy-sync"
)

// OvsDpdkResourcePolicyReconciler reconciles OvsDpdkResourcePolicy objects.
type OvsDpdkResourcePolicyReconciler struct {
	client.Client
	nodeName           string
	namespace          string
	log                klog.Logger
	deviceStateManager *devicestate.DeviceState
}

// NewOvsDpdkResourcePolicyReconciler creates a new OvsDpdkResourcePolicyReconciler.
func NewOvsDpdkResourcePolicyReconciler(
	c client.Client,
	nodeName, namespace string,
	deviceStateManager *devicestate.DeviceState,
) *OvsDpdkResourcePolicyReconciler {
	return &OvsDpdkResourcePolicyReconciler{
		Client:             c,
		nodeName:           nodeName,
		namespace:          namespace,
		log:                klog.Background().WithName("OvsDpdkResourcePolicyReconciler"),
		deviceStateManager: deviceStateManager,
	}
}

// Reconcile handles reconciliation of OvsDpdkResourcePolicy objects.
// It finds all policies in the watched namespace that match this node and
// forwards the consolidated bridge configuration to the device-state manager.
func (r *OvsDpdkResourcePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.Info("Starting reconcile", "request", req.NamespacedName, "watchedNamespace", r.namespace)

	// Fetch the node so we can match NodeSelector terms against its labels and fields.
	node := &corev1.Node{}
	if err := r.Get(ctx, types.NamespacedName{Name: r.nodeName}, node); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.Error(err, "Node not found, requeuing", "nodeName", r.nodeName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		r.log.Error(err, "Failed to get node", "nodeName", r.nodeName)
		return ctrl.Result{}, err
	}

	// List all OvsDpdkResourcePolicy objects in the watched namespace.
	policyList := &ovsdpdkdrav1alpha1.OvsDpdkResourcePolicyList{}
	if err := r.List(ctx, policyList, client.InNamespace(r.namespace)); err != nil {
		r.log.Error(err, "Failed to list OvsDpdkResourcePolicy objects", "namespace", r.namespace)
		return ctrl.Result{}, err
	}

	// Collect bridge specs and vhost-user config from all matching policies.
	var bridges []ovsdpdkdrav1alpha1.BridgeSpec
	var vhostUser *ovsdpdkdrav1alpha1.VhostUserSpec
	for i := range policyList.Items {
		policy := &policyList.Items[i]
		if !r.matchesNodeSelector(node, policy.Spec.NodeSelector) {
			r.log.V(2).Info("Policy does not match node, skipping",
				"policy", policy.Name, "nodeName", r.nodeName)
			continue
		}
		r.log.V(2).Info("Policy matches node, collecting bridges",
			"policy", policy.Name, "bridges", len(policy.Spec.Bridges))
		bridges = append(bridges, policy.Spec.Bridges...)
		if vhostUser == nil && policy.Spec.VhostUser != nil {
			vhostUser = policy.Spec.VhostUser
		}
	}

	r.log.Info("Reconciled policies", "matchingBridges", len(bridges))

	if err := r.deviceStateManager.UpdatePolicyDevices(ctx, bridges, vhostUser); err != nil {
		r.log.Error(err, "Failed to update policy devices")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// matchesNodeSelector returns true if the given node satisfies the
// NodeSelector. A nil selector matches all nodes.
func (r *OvsDpdkResourcePolicyReconciler) matchesNodeSelector(
	node *corev1.Node,
	ns *corev1.NodeSelector,
) bool {
	if ns == nil || len(ns.NodeSelectorTerms) == 0 {
		return true
	}

	selector, err := nodeaffinity.NewNodeSelector(ns)
	if err != nil {
		r.log.Error(err, "Failed to parse NodeSelector")
		return false
	}
	return selector.Match(node)
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *OvsDpdkResourcePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// delayedEnqueue adds a reconcile request after a short delay to coalesce
	// rapid successive events into a single reconcile.
	delayedEnqueue := func(q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
		q.AddAfter(reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: r.namespace,
			Name:      resourcePolicySyncEventName,
		}}, time.Second)
	}

	policyEventHandler := handler.Funcs{
		CreateFunc: func(_ context.Context, e event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.log.Info("Enqueuing sync for policy create", "policy", e.Object.GetName())
			delayedEnqueue(q)
		},
		UpdateFunc: func(_ context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.log.Info("Enqueuing sync for policy update", "policy", e.ObjectNew.GetName())
			delayedEnqueue(q)
		},
		DeleteFunc: func(_ context.Context, e event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.log.Info("Enqueuing sync for policy delete", "policy", e.Object.GetName())
			delayedEnqueue(q)
		},
		GenericFunc: func(_ context.Context, e event.TypedGenericEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.log.Info("Enqueuing sync for policy generic event", "policy", e.Object.GetName())
			delayedEnqueue(q)
		},
	}

	nodeEventHandler := handler.Funcs{
		CreateFunc: func(_ context.Context, e event.TypedCreateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if e.Object.GetName() == r.nodeName {
				r.log.Info("Enqueuing sync for node create", "node", e.Object.GetName())
				delayedEnqueue(q)
			}
		},
		UpdateFunc: func(_ context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if e.ObjectNew.GetName() == r.nodeName {
				if !labels.Equals(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
					r.log.Info("Enqueuing sync for node label change", "node", e.ObjectNew.GetName())
					delayedEnqueue(q)
				}
			}
		},
		DeleteFunc: func(_ context.Context, e event.TypedDeleteEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if e.Object.GetName() == r.nodeName {
				r.log.Info("Enqueuing sync for node delete", "node", e.Object.GetName())
				delayedEnqueue(q)
			}
		},
	}

	// Trigger an initial reconcile at startup.
	startupChan := make(chan event.GenericEvent, 1)
	startupChan <- event.GenericEvent{Object: &ovsdpdkdrav1alpha1.OvsDpdkResourcePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: resourcePolicySyncEventName, Namespace: r.namespace},
	}}
	close(startupChan)

	// Only process policy events from our watched namespace.
	namespacePredicate := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetNamespace() == r.namespace
	})

	nodeMetadata := &metav1.PartialObjectMetadata{}
	nodeMetadata.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Node"))

	return ctrl.NewControllerManagedBy(mgr).
		For(&ovsdpdkdrav1alpha1.OvsDpdkResourcePolicy{}, builder.WithPredicates(namespacePredicate)).
		Watches(nodeMetadata, nodeEventHandler).
		Watches(&ovsdpdkdrav1alpha1.OvsDpdkResourcePolicy{},
			policyEventHandler,
			builder.WithPredicates(namespacePredicate)).
		WatchesRawSource(source.Channel(startupChan, &handler.EnqueueRequestForObject{})).
		Complete(r)
}
