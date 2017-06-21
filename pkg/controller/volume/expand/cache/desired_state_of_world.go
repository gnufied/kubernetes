/*
Copyright 2016 The Kubernetes Authors.

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

package cache

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/util/types"
)

type DesiredStateOfWorld interface {
	AddPvcUpdate(newPvc *v1.PersistentVolumeClaim, oldPvc *v1.PersistentVolumeClaim, spec *volume.Spec)
	GetPvcsWithResizeRequest() []*PvcWithResizeRequest
	MarkAsResized(*PvcWithResizeRequest)
}

type desiredStateOfWorld struct {
	pvcrs map[types.UniquePvcName]*PvcWithResizeRequest
}

type PvcWithResizeRequest struct {
	PVC          *v1.PersistentVolumeClaim
	VolumeSpec   *volume.Spec
	CurrentSize  resource.Quantity
	ExpectedSize resource.Quantity
	ResizeDone   bool
}

func (pvcr *PvcWithResizeRequest) UniquePvcKey() types.UniquePvcName {
	return types.UniquePvcName(pvcr.PVC.UID)
}

func NewDesiredStateOfWorld() DesiredStateOfWorld {
	dsow := &desiredStateOfWorld{}
	return dsow
}

func (dsow *desiredStateOfWorld) AddPvcUpdate(newPvc *v1.PersistentVolumeClaim, oldPvc *v1.PersistentVolumeClaim, spec *volume.Spec) {
	newSize := newPvc.Spec.Resources.Requests[v1.ResourceStorage]
	oldSize := newPvc.Spec.Resources.Requests[v1.ResourceStorage]

	if newSize.Cmp(oldSize) > 0 {
		glog.Infof("hekumar -- pvc to desired state of world")
		pvcRequest := &PvcWithResizeRequest{
			PVC:          newPvc,
			CurrentSize:  newPvc.Status.Capacity[v1.ResourceStorage],
			ExpectedSize: newSize,
			VolumeSpec:   spec,
			ResizeDone:   false,
		}
		dsow.pvcrs[types.UniquePvcName(newPvc.UID)] = pvcRequest
	}
}

// Return Pvcrs that require resize
func (dsow *desiredStateOfWorld) GetPvcsWithResizeRequest() []*PvcWithResizeRequest {
	pvcrs := []*PvcWithResizeRequest{}
	for _, pvcr := range dsow.pvcrs {
		if !pvcr.ResizeDone {
			pvcrs = append(pvcrs, pvcr)
		}
	}
	return pvcrs
}

func (dsow *desiredStateOfWorld) MarkAsResized(pvcr *PvcWithResizeRequest) {
	pvcUniqueName := pvcr.UniquePvcKey()

	if pvcr, ok := dsow.pvcrs[pvcUniqueName]; ok {
		pvcr.ResizeDone = true
	}
}
