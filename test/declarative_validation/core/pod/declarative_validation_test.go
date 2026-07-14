/*
Copyright 2025 The Kubernetes Authors.

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

package pod

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	podtest "k8s.io/kubernetes/pkg/api/pod/testing"
	apitesting "k8s.io/kubernetes/pkg/api/testing"
	api "k8s.io/kubernetes/pkg/apis/core"
	registry "k8s.io/kubernetes/pkg/registry/core/pod"
	"k8s.io/kubernetes/test/declarative_validation/meta"
)

func TestDeclarativeValidate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
			APIPrefix:         "api",
			APIGroup:          "",
			APIVersion:        apiVersion,
			IsResourceRequest: true,
			Verb:              "create",
		})
		testCases := map[string]struct {
			input        *api.Pod
			expectedErrs field.ErrorList
		}{
			"valid": {
				input: podtest.MakePod("foo"),
			},
			"valid toleration key": {
				input: podtest.MakePod("foo", podtest.SetTolerations(
					api.Toleration{Key: "example.com/valid-key", Operator: api.TolerationOpExists},
				)),
			},
			"valid toleration key without prefix": {
				input: podtest.MakePod("foo", podtest.SetTolerations(
					api.Toleration{Key: "simple-key", Operator: api.TolerationOpExists},
				)),
			},
			"empty toleration key (match all, skipped)": {
				input: podtest.MakePod("foo", podtest.SetTolerations(
					api.Toleration{Operator: api.TolerationOpExists},
				)),
			},
			"invalid toleration key format": {
				input: podtest.MakePod("foo", podtest.SetTolerations(
					api.Toleration{Key: "invalid key", Operator: api.TolerationOpExists},
				)),
				expectedErrs: field.ErrorList{
					field.Invalid(field.NewPath("spec", "tolerations").Index(0).Child("key"), nil, "").WithOrigin("format=k8s-label-key").MarkAlpha(),
				},
			},
		}
		for k, tc := range testCases {
			t.Run(k, func(t *testing.T) {
				apitesting.VerifyValidationEquivalence(t, ctx, tc.input, registry.Strategy, tc.expectedErrs)
			})
		}
		obj := *podtest.MakePod("foo")
		meta.RunObjectMetaTestCases(t, ctx, &obj, registry.Strategy, meta.WithStringentFinalizerValidation())
	}
}

func TestDeclarativeValidateUpdate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
			APIPrefix:         "api",
			APIGroup:          "",
			APIVersion:        apiVersion,
			Name:              "valid-obj",
			IsResourceRequest: true,
			Verb:              "update",
		})
		updateObj := *podtest.MakePod("foo")
		meta.RunObjectMetaUpdateTestCases(t, ctx, &updateObj, registry.Strategy, meta.WithStringentFinalizerValidation())
	}
}

func TestDeclarativeValidateStatusUpdate(t *testing.T) {
	for _, apiVersion := range apiVersions {
		t.Run(apiVersion, func(t *testing.T) {
			testDeclarativeValidateStatusUpdate(t, apiVersion)
		})
	}
}

func testDeclarativeValidateStatusUpdate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "api",
		APIGroup:          "",
		APIVersion:        apiVersion,
		Resource:          "pods",
		Subresource:       "status",
		Name:              "foo",
		IsResourceRequest: true,
		Verb:              "update",
	})

	validCondition := api.VolumeHealthCondition{
		Status: api.VolumeHealthDegraded,
		Reason: "DiskSlow",
	}

	testCases := map[string]struct {
		old          api.Pod
		update       api.Pod
		expectedErrs field.ErrorList
	}{
		// status.volumeHealth[*].healthConditions[*].reason — required, maxLength=256
		"valid volumeHealth reason, max length": {
			old: *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(api.VolumeHealthCondition{
				Status: api.VolumeHealthDegraded, Reason: strings.Repeat("a", 256),
			}),
		},
		"invalid volumeHealth reason, empty": {
			old: *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(api.VolumeHealthCondition{
				Status: api.VolumeHealthDegraded, Reason: "",
			}),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("status", "volumeHealth").Index(0).Child("healthConditions").Index(0).Child("reason"), "").MarkCoveredByDeclarative(),
			},
		},
		"invalid volumeHealth reason, too long": {
			old: *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(api.VolumeHealthCondition{
				Status: api.VolumeHealthDegraded, Reason: strings.Repeat("a", 257),
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "volumeHealth").Index(0).Child("healthConditions").Index(0).Child("reason"), "", 256).MarkCoveredByDeclarative().WithOrigin("maxLength"),
			},
		},
		// status.volumeHealth[*].healthConditions[*].message — maxLength=1024
		"valid volumeHealth message, max length": {
			old: *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(api.VolumeHealthCondition{
				Status: api.VolumeHealthDegraded, Reason: "DiskSlow", Message: strings.Repeat("a", 1024),
			}),
		},
		"invalid volumeHealth message, too long": {
			old: *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(api.VolumeHealthCondition{
				Status: api.VolumeHealthDegraded, Reason: "DiskSlow", Message: strings.Repeat("a", 1025),
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "volumeHealth").Index(0).Child("healthConditions").Index(0).Child("message"), "", 1024).MarkCoveredByDeclarative().WithOrigin("maxLength"),
			},
		},
		// valid: no conditions at all
		"valid volumeHealth, no conditions": {
			old:    *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(),
		},
		// valid: full condition
		"valid volumeHealth, full condition": {
			old:    *podtest.MakePod("foo"),
			update: mkPodWithVolumeHealth(validCondition),
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

func mkPodWithVolumeHealth(conditions ...api.VolumeHealthCondition) api.Pod {
	pod := *podtest.MakePod("foo", podtest.SetVolumes(api.Volume{
		Name:         "vol1",
		VolumeSource: api.VolumeSource{EmptyDir: &api.EmptyDirVolumeSource{}},
	}))
	pod.Status.VolumeHealth = []api.PodVolumeHealth{{
		Name:             "vol1",
		HealthConditions: conditions,
	}}
	return pod
}
