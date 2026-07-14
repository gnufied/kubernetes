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

package csinode

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	apitesting "k8s.io/kubernetes/pkg/api/testing"
	storage "k8s.io/kubernetes/pkg/apis/storage"
	registry "k8s.io/kubernetes/pkg/registry/storage/csinode"
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
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "apis",
		APIGroup:          "storage.k8s.io",
		APIVersion:        apiVersion,
		Resource:          "csinodes",
		IsResourceRequest: true,
		Verb:              "create",
	})

	obj := mkCSINode()
	meta.RunObjectMetaTestCases(t, ctx, &obj, registry.Strategy, meta.WithStringentFinalizerValidation())
}

func testDeclarativeValidateUpdate(t *testing.T, apiVersion string) {
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "apis",
		APIGroup:          "storage.k8s.io",
		APIVersion:        apiVersion,
		Resource:          "csinodes",
		Name:              "valid-obj",
		IsResourceRequest: true,
		Verb:              "update",
	})

	updateObj := mkCSINode()
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
	ctx := genericapirequest.WithRequestInfo(genericapirequest.NewDefaultContext(), &genericapirequest.RequestInfo{
		APIPrefix:         "apis",
		APIGroup:          "storage.k8s.io",
		APIVersion:        apiVersion,
		Resource:          "csinodes",
		Subresource:       "status",
		Name:              "valid-obj",
		IsResourceRequest: true,
		Verb:              "update",
	})

	validCondition := storage.StorageHealthCondition{
		Name:   "test.driver",
		Status: storage.StorageDegraded,
		Reason: "PoolFull",
	}

	testCases := map[string]struct {
		old          storage.CSINode
		update       storage.CSINode
		expectedErrs field.ErrorList
	}{
		// status.storageHealth — maxItems=16
		"valid storageHealth, at limit": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				for i := 0; i < 16; i++ {
					node.Status.StorageHealth = append(node.Status.StorageHealth, storage.StorageHealthCondition{
						Name:   "test.driver",
						Status: storage.StorageDegraded,
						Reason: string(rune('A' + i)),
					})
				}
			}),
		},
		"invalid storageHealth, too many": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				for i := 0; i < 17; i++ {
					node.Status.StorageHealth = append(node.Status.StorageHealth, storage.StorageHealthCondition{
						Name:   "test.driver",
						Status: storage.StorageDegraded,
						Reason: string(rune('A' + i)),
					})
				}
			}),
			expectedErrs: field.ErrorList{
				field.TooMany(field.NewPath("status", "storageHealth"), 17, 16).WithOrigin("maxItems"),
			},
		},
		// status.storageHealth[*].name — required
		"valid storageHealth name": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{validCondition}
			}),
		},
		"invalid storageHealth name, empty": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "", Status: storage.StorageDegraded, Reason: "PoolFull",
				}}
			}),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("status", "storageHealth").Index(0).Child("name"), "").MarkCoveredByDeclarative(),
			},
		},
		// status.storageHealth[*].status — required
		"invalid storageHealth status, empty": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: "", Reason: "PoolFull",
				}}
			}),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("status", "storageHealth").Index(0).Child("status"), "").MarkCoveredByDeclarative(),
				field.NotSupported[storage.StorageHealthStatusType](field.NewPath("status", "storageHealth").Index(0).Child("status"), storage.StorageHealthStatusType(""), nil),
			},
		},
		// status.storageHealth[*].reason — required, maxLength=256
		"valid storageHealth reason, max length": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: storage.StorageDegraded, Reason: strings.Repeat("a", 256),
				}}
			}),
		},
		"invalid storageHealth reason, empty": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: storage.StorageDegraded, Reason: "",
				}}
			}),
			expectedErrs: field.ErrorList{
				field.Required(field.NewPath("status", "storageHealth").Index(0).Child("reason"), "").MarkCoveredByDeclarative(),
			},
		},
		"invalid storageHealth reason, too long": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: storage.StorageDegraded, Reason: strings.Repeat("a", 257),
				}}
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "storageHealth").Index(0).Child("reason"), "", 256).MarkCoveredByDeclarative().WithOrigin("maxLength"),
			},
		},
		// status.storageHealth[*].message — maxLength=1024
		"valid storageHealth message, max length": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: storage.StorageDegraded, Reason: "PoolFull", Message: strings.Repeat("a", 1024),
				}}
			}),
		},
		"invalid storageHealth message, too long": {
			old: mkCSINode(),
			update: mkCSINode(func(node *storage.CSINode) {
				node.Status.StorageHealth = []storage.StorageHealthCondition{{
					Name: "test.driver", Status: storage.StorageDegraded, Reason: "PoolFull", Message: strings.Repeat("a", 1025),
				}}
			}),
			expectedErrs: field.ErrorList{
				field.TooLong(field.NewPath("status", "storageHealth").Index(0).Child("message"), "", 1024).MarkCoveredByDeclarative().WithOrigin("maxLength"),
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

func mkCSINode(tweaks ...func(node *storage.CSINode)) storage.CSINode {
	node := storage.CSINode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-obj",
		},
		Spec: storage.CSINodeSpec{
			Drivers: []storage.CSINodeDriver{
				{
					Name:   "foo",
					NodeID: "bar",
				},
			},
		},
	}
	for _, tweak := range tweaks {
		tweak(&node)
	}
	return node
}
