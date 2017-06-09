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
	"k8s.io/kubernetes/pkg/api/v1"
)

// implement interface of actual state of world
type ActualStateOfWorld interface {
	AddPvcUpdate(newPvc *v1.PersistentVolumeClaim, oldPvc *v1.PersistentVolumeClaim)
}

type actualStateOfWorld struct {
}

func NewActualStateOfWorld() ActualStateOfWorld {
	asow := &actualStateOfWorld{}
	return asow
}

func (asow *actualStateOfWorld) AddPvcUpdate(newPvc *v1.PersistentVolumeClaim, oldPvc *v1.PersistentVolumeClaim) {

}
