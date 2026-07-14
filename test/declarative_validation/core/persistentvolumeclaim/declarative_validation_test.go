/*
Copyright The Kubernetes Authors.

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

package persistentvolumeclaim

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	apitesting "k8s.io/kubernetes/pkg/api/testing"
	core "k8s.io/kubernetes/pkg/apis/core"
	registry "k8s.io/kubernetes/pkg/registry/core/persistentvolumeclaim"
	"k8s.io/kubernetes/test/declarative_validation/meta"
)

func TestDeclarativeValidate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidate(t, apiVersion)
		})
	}
}

func TestDeclarativeValidateUpdate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidateUpdate(t, apiVersion)
		})
	}
}

func testDeclarativeValidate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithNamespace(genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "api",
		APIGroup:          "",
		APIVersion:        apiVersion,
		Resource:          "persistentvolumeclaims",
		IsResourceRequest: true,
		Verb:              "create",
	}), metav1.NamespaceDefault)

	obj := mkValidPersistentVolumeClaim()
	meta.RunObjectMetaTestCases(t, ctx, &obj, registry.Strategy, meta.WithStringentFinalizerValidation())
}

func testDeclarativeValidateUpdate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithNamespace(genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "api",
		APIGroup:          "",
		APIVersion:        apiVersion,
		Resource:          "persistentvolumeclaims",
		Name:              "valid-obj",
		IsResourceRequest: true,
		Verb:              "update",
	}), metav1.NamespaceDefault)

	updateObj := mkValidPersistentVolumeClaim()
	meta.RunObjectMetaUpdateTestCases(t, ctx, &updateObj, registry.Strategy, meta.WithStringentFinalizerValidation())
}

func TestDeclarativeValidateStatusUpdate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidateStatusUpdate(t, apiVersion)
		})
	}
}

func testDeclarativeValidateStatusUpdate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithNamespace(genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "api",
		APIGroup:          "",
		APIVersion:        apiVersion,
		Resource:          "persistentvolumeclaims",
		Subresource:       "status",
		Name:              "valid-obj",
		IsResourceRequest: true,
		Verb:              "update",
	}), metav1.NamespaceDefault)

	testCases := map[string]struct {
		old          core.PersistentVolumeClaim
		update       core.PersistentVolumeClaim
		expectedErrs field.ErrorList
	}{
		// status.healthStatus.healthConditions — maxItems=16
		"valid healthConditions, at limit": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				for i := range 16 {
					pvc.Status.HealthStatus.HealthConditions = append(pvc.Status.HealthStatus.HealthConditions, core.VolumeHealthCondition{
						Status: core.VolumeHealthDegraded, Reason: string(rune('A' + i)),
					})
				}
			}),
		},
		"invalid healthConditions, too many": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				for i := range 17 {
					pvc.Status.HealthStatus.HealthConditions = append(pvc.Status.HealthStatus.HealthConditions, core.VolumeHealthCondition{
						Status: core.VolumeHealthDegraded, Reason: string(rune('A' + i)),
					})
				}
			}),
			expectedErrs: field.ErrorList{
				field.TooMany(field.NewPath("status", "healthStatus", "healthConditions"), 17, 16).WithOrigin("maxItems"),
			},
		},
		// status.healthStatus.healthConditions[*].reason — required, maxLength=256
		"valid healthCondition reason, max length": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				pvc.Status.HealthStatus.HealthConditions = []core.VolumeHealthCondition{{
					Status: core.VolumeHealthDegraded, Reason: strings.Repeat("a", 256),
				}}
			}),
		},
		"invalid healthCondition reason, empty": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				pvc.Status.HealthStatus.HealthConditions = []core.VolumeHealthCondition{{
					Status: core.VolumeHealthDegraded, Reason: "",
				}}
			}),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("status", "healthStatus", "healthConditions").Index(0).Child("reason"), "").MarkCoveredByDeclarative(),
			},
		},
		"invalid healthCondition reason, too long": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				pvc.Status.HealthStatus.HealthConditions = []core.VolumeHealthCondition{{
					Status: core.VolumeHealthDegraded, Reason: strings.Repeat("a", 257),
				}}
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "healthStatus", "healthConditions").Index(0).Child("reason"), "", 256).MarkCoveredByDeclarative().WithOrigin("maxLength"),
			},
		},
		// status.healthStatus.healthConditions[*].message — maxLength=1024
		"valid healthCondition message, max length": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				pvc.Status.HealthStatus.HealthConditions = []core.VolumeHealthCondition{{
					Status: core.VolumeHealthDegraded, Reason: "DiskSlow", Message: strings.Repeat("a", 1024),
				}}
			}),
		},
		"invalid healthCondition message, too long": {
			old: mkValidPersistentVolumeClaim(),
			update: mkValidPersistentVolumeClaimWithStatus(func(pvc *core.PersistentVolumeClaim) {
				pvc.Status.HealthStatus.HealthConditions = []core.VolumeHealthCondition{{
					Status: core.VolumeHealthDegraded, Reason: "DiskSlow", Message: strings.Repeat("a", 1025),
				}}
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "healthStatus", "healthConditions").Index(0).Child("message"), "", 1024).MarkCoveredByDeclarative().WithOrigin("maxLength"),
			},
		},
	}

	for k, tc := range testCases {
		t.Run(k, func(t *testing.T) {
			tc.old.ObjectMeta.ResourceVersion = "1"
			tc.update.ObjectMeta.ResourceVersion = "1"
			apitesting.VerifyUpdateValidationEquivalence(t, ctx, &tc.update, &tc.old, registry.StatusStrategy, tc.expectedErrs, apitesting.WithSubResources("status"))
		})
	}
}

func mkValidPersistentVolumeClaimWithStatus(tweaks ...func(pvc *core.PersistentVolumeClaim)) core.PersistentVolumeClaim {
	pvc := mkValidPersistentVolumeClaim()
	pvc.Status.HealthStatus = &core.VolumeHealthStatus{}
	for _, tweak := range tweaks {
		tweak(&pvc)
	}
	return pvc
}

func mkValidPersistentVolumeClaim() core.PersistentVolumeClaim {
	return core.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-obj",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: core.PersistentVolumeClaimSpec{
			AccessModes: []core.PersistentVolumeAccessMode{core.ReadWriteOnce},
			Resources: core.VolumeResourceRequirements{
				Requests: core.ResourceList{core.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
}
