/*
Copyright 2026 The Soteria Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package admission

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/soteria-project/soteria/pkg/engine"
)

// ValidateDRPlanPath is the webhook endpoint path for DRPlan validation.
const ValidateDRPlanPath = "/validate-soteria-io-v1alpha1-drplan"

// ValidateVMPath is the webhook endpoint path for VirtualMachine validation.
const ValidateVMPath = "/validate-kubevirt-io-v1-virtualmachine"

// SetupDRPlanWebhook registers the DRPlan validating webhook with the manager.
func SetupDRPlanWebhook(mgr ctrl.Manager) error {
	validator := &DRPlanValidator{
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(ValidateDRPlanPath,
		&webhook.Admission{Handler: validator})

	return nil
}

// SetupVMWebhook registers the VirtualMachine validating webhook with the manager.
func SetupVMWebhook(
	mgr ctrl.Manager,
	nsLookup engine.NamespaceLookup,
	vmDiscoverer engine.VMDiscoverer,
) error {
	validator := &VMValidator{
		NSLookup:     nsLookup,
		Client:       mgr.GetClient(),
		VMDiscoverer: vmDiscoverer,
		decoder:      admission.NewDecoder(mgr.GetScheme()),
	}

	mgr.GetWebhookServer().Register(ValidateVMPath,
		&webhook.Admission{Handler: validator})

	return nil
}
