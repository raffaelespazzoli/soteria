//go:build integration

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

// suite_test.go boots an envtest environment with Soteria CRDs and the persona
// ClusterRoles applied. Tests use user impersonation to validate RBAC
// enforcement for each persona (viewer, editor, operator).

package rbac_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	sigsyaml "sigs.k8s.io/yaml"

	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	adminClient client.Client
	adminConfig *rest.Config
	testScheme  *runtime.Scheme
	testEnv     *envtest.Environment
)

func TestMain(m *testing.M) {
	ctrl.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	testScheme = runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(testScheme)
	_ = apiextensionsv1.AddToScheme(testScheme)
	_ = rbacv1.AddToScheme(testScheme)

	testEnv = &envtest.Environment{
		Scheme: testScheme,
		CRDInstallOptions: envtest.CRDInstallOptions{
			CRDs: []*apiextensionsv1.CustomResourceDefinition{
				drplanCRD(),
				drexecutionCRD(),
				drgroupstatusCRD(),
			},
		},
		BinaryAssetsDirectory: os.Getenv("KUBEBUILDER_ASSETS"),
	}

	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("starting envtest: %v", err))
	}
	adminConfig = cfg

	adminClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	if err != nil {
		panic(fmt.Sprintf("creating admin client: %v", err))
	}

	ctx := context.Background()

	// Apply persona ClusterRoles
	for _, role := range personaClusterRoles() {
		if err := adminClient.Create(ctx, role); err != nil {
			panic(fmt.Sprintf("creating ClusterRole %s: %v", role.Name, err))
		}
	}

	exitCode := m.Run()

	if err := testEnv.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "stopping envtest: %v\n", err)
	}
	os.Exit(exitCode)
}

// impersonatedClient returns a controller-runtime client that impersonates the
// given user with the specified groups. The admin config can impersonate because
// envtest's client cert belongs to system:masters.
func impersonatedClient(user string, groups []string) (client.Client, error) {
	cfg := rest.CopyConfig(adminConfig)
	cfg.Impersonate = rest.ImpersonationConfig{
		UserName: user,
		Groups:   groups,
	}
	return client.New(cfg, client.Options{Scheme: testScheme})
}

// bindRole creates a ClusterRoleBinding associating the given user with the
// named ClusterRole.
func bindRole(ctx context.Context, bindingName, roleName, userName string) error {
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind: rbacv1.UserKind,
			Name: userName,
		}},
	}
	return adminClient.Create(ctx, binding)
}

func drplanGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "soteria.io", Version: "v1alpha1", Resource: "drplans"}
}

func drexecutionGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "soteria.io", Version: "v1alpha1", Resource: "drexecutions"}
}

// personaClusterRoles loads the shipped persona ClusterRole YAML manifests so
// tests always validate the real config/rbac definitions (no Go duplication).
func personaClusterRoles() []*rbacv1.ClusterRole {
	rbacDir := filepath.Join("..", "..", "..", "config", "rbac")
	files := []string{
		"soteria_viewer_role.yaml",
		"soteria_editor_role.yaml",
		"soteria_operator_role.yaml",
	}

	roles := make([]*rbacv1.ClusterRole, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(rbacDir, f))
		if err != nil {
			panic(fmt.Sprintf("reading persona role %s: %v", f, err))
		}
		var role rbacv1.ClusterRole
		if err := sigsyaml.UnmarshalStrict(data, &role); err != nil {
			panic(fmt.Sprintf("parsing persona role %s: %v", f, err))
		}
		roles = append(roles, &role)
	}
	return roles
}

func drplanCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "drplans.soteria.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "soteria.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "drplans", Singular: "drplan",
				Kind: "DRPlan", ListKind: "DRPlanList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha1", Served: true, Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec":   {Type: "object", XPreserveUnknownFields: boolPtr(true)},
							"status": {Type: "object", XPreserveUnknownFields: boolPtr(true)},
						},
					},
				},
			}},
		},
	}
}

func drexecutionCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "drexecutions.soteria.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "soteria.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "drexecutions", Singular: "drexecution",
				Kind: "DRExecution", ListKind: "DRExecutionList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha1", Served: true, Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec":   {Type: "object", XPreserveUnknownFields: boolPtr(true)},
							"status": {Type: "object", XPreserveUnknownFields: boolPtr(true)},
						},
					},
				},
			}},
		},
	}
}

func drgroupstatusCRD() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "drgroupstatuses.soteria.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "soteria.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "drgroupstatuses", Singular: "drgroupstatus",
				Kind: "DRGroupStatus", ListKind: "DRGroupStatusList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1alpha1", Served: true, Storage: true,
				Subresources: &apiextensionsv1.CustomResourceSubresources{
					Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
				},
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"spec":   {Type: "object", XPreserveUnknownFields: boolPtr(true)},
							"status": {Type: "object", XPreserveUnknownFields: boolPtr(true)},
						},
					},
				},
			}},
		},
	}
}

func boolPtr(b bool) *bool { return &b }
