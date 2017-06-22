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
package reconciler

import (
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/controller/volume/expand/cache"
	"k8s.io/kubernetes/pkg/volume/util/operationexecutor"
)

type Reconciler interface {
	Run(stopCh <-chan struct{})
}

type reconciler struct {
	loopPeriod          time.Duration
	desiredStateOfWorld cache.DesiredStateOfWorld
	opsExecutor         operationexecutor.OperationExecutor
}

func NewReconciler(
	loopPeriod time.Duration,
	opsExecutor operationexecutor.OperationExecutor,
	dsow cache.DesiredStateOfWorld) Reconciler {
	rc := &reconciler{
		loopPeriod:          loopPeriod,
		opsExecutor:         opsExecutor,
		desiredStateOfWorld: dsow,
	}
	return rc
}

func (rc *reconciler) Run(stopCh <-chan struct{}) {
	wait.Until(rc.reconcile, rc.loopPeriod, stopCh)
}

func (rc *reconciler) reconcile() {
	// Resize PVCs that require resize
	for _, pvcWithResizeRequest := range rc.desiredStateOfWorld.GetPvcsWithResizeRequest() {
		uniqueVolumeKey := v1.UniqueVolumeName(pvcWithResizeRequest.UniquePvcKey())
		if rc.opsExecutor.IsOperationPending(uniqueVolumeKey, "") {
			glog.V(10).Infof("Operation for PVC %v is already pending", pvcWithResizeRequest.UniquePvcKey())
			continue
		}
		growFuncError := rc.opsExecutor.GrowPvc(pvcWithResizeRequest, rc.desiredStateOfWorld)
		if growFuncError != nil {
			glog.Errorf("Error growing pvc with %v", growFuncError)
		}
		glog.Infof("Resizing PVC %s", pvcWithResizeRequest.CurrentSize)
	}
}
