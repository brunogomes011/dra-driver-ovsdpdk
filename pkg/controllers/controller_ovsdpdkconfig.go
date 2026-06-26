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

package controllers

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ovsdpdkdrav1alpha1 "github.com/amorenoz/dra-driver-ovsdpdk/pkg/api/ovsdpdkdra/v1alpha1"
	"github.com/amorenoz/dra-driver-ovsdpdk/pkg/devicestate"
)

// OvsDpdkConfigReconciler reconciles the single OvsDpdkConfig object.
type OvsDpdkConfigReconciler struct {
	client.Client
	configName         string
	log                klog.Logger
	deviceStateManager *devicestate.DeviceState
}

// NewOvsDpdkConfigReconciler creates a new OvsDpdkConfigReconciler.
func NewOvsDpdkConfigReconciler(
	c client.Client,
	configName string,
	deviceStateManager *devicestate.DeviceState,
) *OvsDpdkConfigReconciler {
	return &OvsDpdkConfigReconciler{
		Client:             c,
		configName:         configName,
		log:                klog.Background().WithName("OvsDpdkConfigReconciler"),
		deviceStateManager: deviceStateManager,
	}
}

// Reconcile handles reconciliation of the OvsDpdkConfig object.
func (r *OvsDpdkConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.Info("Starting reconcile", "request", req.NamespacedName)

	if req.Name != r.configName {
		r.log.Info("OvsDpdkConfig has unexpected name", "request", req.NamespacedName, "expected", r.configName)
		return ctrl.Result{}, nil
	}

	config := &ovsdpdkdrav1alpha1.OvsDpdkConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: r.configName}, config); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.Info("OvsDpdkConfig not found", "name", r.configName)
			return ctrl.Result{}, r.deviceStateManager.UpdateConfig(ctx, nil)
		}
		r.log.Error(err, "Failed to get OvsDpdkConfig", "name", r.configName)
		return ctrl.Result{}, err
	}

	if err := r.deviceStateManager.UpdateConfig(ctx, &config.Spec); err != nil {
		r.log.Error(err, "Failed to update config")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *OvsDpdkConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ovsdpdkdrav1alpha1.OvsDpdkConfig{}).
		Complete(r)
}
