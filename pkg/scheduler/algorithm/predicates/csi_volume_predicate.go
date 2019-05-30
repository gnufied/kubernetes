/*
Copyright 2018 The Kubernetes Authors.

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

package predicates

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	storagev1beta1 "k8s.io/api/storage/v1beta1"
	"k8s.io/apimachinery/pkg/util/rand"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	csilib "k8s.io/csi-translation-lib"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/features"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"
	volumeutil "k8s.io/kubernetes/pkg/volume/util"
)

// CSIMaxVolumeLimitChecker defines predicate needed for counting CSI volumes
type CSIMaxVolumeLimitChecker struct {
	pvInfo  PersistentVolumeInfo
	pvcInfo PersistentVolumeClaimInfo
	scInfo  StorageClassInfo

	randomVolumeIDPrefix string
}

// NewCSIMaxVolumeLimitPredicate returns a predicate for counting CSI volumes
func NewCSIMaxVolumeLimitPredicate(
	pvInfo PersistentVolumeInfo, pvcInfo PersistentVolumeClaimInfo, scInfo StorageClassInfo) FitPredicate {
	c := &CSIMaxVolumeLimitChecker{
		pvInfo:               pvInfo,
		pvcInfo:              pvcInfo,
		scInfo:               scInfo,
		randomVolumeIDPrefix: rand.String(32),
	}
	return c.attachableLimitPredicate
}

func (c *CSIMaxVolumeLimitChecker) attachableLimitPredicate(
	pod *v1.Pod, meta PredicateMetadata, nodeInfo *schedulernodeinfo.NodeInfo) (bool, []PredicateFailureReason, error) {

	// if feature gate is disable we return
	if !utilfeature.DefaultFeatureGate.Enabled(features.AttachVolumeLimit) {
		return true, nil, nil
	}
	// If a pod doesn't have any volume attached to it, the predicate will always be true.
	// Thus we make a fast path for it, to avoid unnecessary computations in this case.
	if len(pod.Spec.Volumes) == 0 {
		return true, nil, nil
	}

	nodeVolumeLimits := nodeInfo.VolumeLimits()

	// if node does not have volume limits this predicate should exit
	if len(nodeVolumeLimits) == 0 {
		return true, nil, nil
	}

	// a map of unique volume name/csi volume handle and volume limit key
	newVolumes := make(map[string]string)
	if err := c.filterAttachableVolumes(nodeInfo, pod.Spec.Volumes, pod.Namespace, newVolumes); err != nil {
		return false, nil, err
	}

	if len(newVolumes) == 0 {
		return true, nil, nil
	}

	// a map of unique volume name/csi volume handle and volume limit key
	attachedVolumes := make(map[string]string)
	for _, existingPod := range nodeInfo.Pods() {
		if err := c.filterAttachableVolumes(nodeInfo, existingPod.Spec.Volumes, existingPod.Namespace, attachedVolumes); err != nil {
			return false, nil, err
		}
	}

	newVolumeCount := map[string]int{}
	attachedVolumeCount := map[string]int{}

	for volumeName, volumeLimitKey := range attachedVolumes {
		if _, ok := newVolumes[volumeName]; ok {
			delete(newVolumes, volumeName)
		}
		attachedVolumeCount[volumeLimitKey]++
	}

	for _, volumeLimitKey := range newVolumes {
		newVolumeCount[volumeLimitKey]++
	}

	for volumeLimitKey, count := range newVolumeCount {
		maxVolumeLimit, ok := nodeVolumeLimits[v1.ResourceName(volumeLimitKey)]
		if ok {
			currentVolumeCount := attachedVolumeCount[volumeLimitKey]
			if currentVolumeCount+count > int(maxVolumeLimit) {
				return false, []PredicateFailureReason{ErrMaxVolumeCountExceeded}, nil
			}
		}
	}

	return true, nil, nil
}

func (c *CSIMaxVolumeLimitChecker) filterAttachableVolumes(
	nodeInfo *schedulernodeinfo.NodeInfo, volumes []v1.Volume, namespace string, result map[string]string) error {

	for _, vol := range volumes {
		// CSI volumes can only be used as persistent volumes
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		pvcName := vol.PersistentVolumeClaim.ClaimName

		if pvcName == "" {
			return fmt.Errorf("PersistentVolumeClaim had no name")
		}

		pvc, err := c.pvcInfo.GetPersistentVolumeClaimInfo(namespace, pvcName)

		if err != nil {
			klog.V(4).Infof("Unable to look up PVC info for %s/%s", namespace, pvcName)
			continue
		}

		csiNode := nodeInfo.CSINode()
		driverName, volumeHandle := c.getCSIDriverInfo(csiNode, pvc)
		// if we can't find driver name or volume handle - we don't count this volume.
		if driverName == "" || volumeHandle == "" {
			continue
		}
		volumeLimitKey := volumeutil.GetCSIAttachLimitKey(driverName)
		result[volumeHandle] = volumeLimitKey

	}
	return nil
}

func (c *CSIMaxVolumeLimitChecker) getCSIDriverInfo(csiNode *storagev1beta1.CSINode, pvc *v1.PersistentVolumeClaim) (string, string) {
	pvName := pvc.Spec.VolumeName
	namespace := pvc.Namespace
	pvcName := pvc.Name

	if pvName == "" {
		klog.V(5).Infof("Persistent volume had no name for claim %s/%s", namespace, pvcName)
		return c.getDriverNameFromSC(pvc)
	}

	pv, err := c.pvInfo.GetPersistentVolumeInfo(pvName)
	if err != nil {
		klog.V(5).Infof("Unable to look up PV info for PVC %s/%s and PV %s", namespace, pvcName, pvName)
		// If we can't fetch PV associated with PVC, may be it got deleted
		// or PVC was prebound to a PVC that hasn't been created yet.
		// fallback to using StorageClass for volume counting
		return c.getDriverNameFromSC(pvc)
	}

	csiSource := pv.Spec.PersistentVolumeSource.CSI
	if csiSource == nil {
		if !csilib.IsPVMigratable(pv) {
			klog.V(5).Infof("Non-CSI volume %s/%s is not migratable", namespace, pvcName)
			return "", ""
		}

		pluginName, err := csilib.GetInTreePluginNameFromSpec(pv, nil)
		if err != nil {
			klog.V(5).Infof("Unable to look up plugin name from PV spec: %v", err)
			return "", ""
		}

		if !isCSIMigrationOn(csiNode, pluginName) {
			klog.V(5).Infof("CSI Migration of plugin %s is not enabled", pluginName)
			return "", ""
		}

		csiPV, err := csilib.TranslateInTreePVToCSI(pv)
		if err != nil {
			klog.V(5).Infof("Unable to translate in-tree volume to CSI: %v", err)
			return "", ""
		}

		if csiPV.Spec.PersistentVolumeSource.CSI == nil {
			klog.V(5).Infof("Unable to get a valid volume source for translated PV %s", pvName)
			return "", ""
		}

		csiSource = csiPV.Spec.PersistentVolumeSource.CSI
	}

	return csiSource.Driver, csiSource.VolumeHandle
}

func (c *CSIMaxVolumeLimitChecker) getDriverNameFromSC(pvc *v1.PersistentVolumeClaim) (string, string) {
	namespace := pvc.Namespace
	pvcName := pvc.Name
	scName := pvc.Spec.StorageClassName

	placeHolderCSIDriver := ""
	placeHolderHandle := ""
	if scName == nil {
		// if StorageClass is not set or found, then PVC must be using immediate binding mode
		// and hence it must be bound before scheduling. So it is safe to not count it.
		klog.V(5).Infof("pvc %s/%s has no storageClass", namespace, pvcName)
		return placeHolderCSIDriver, placeHolderHandle
	}

	storageClass, err := c.scInfo.GetStorageClassInfo(*scName)
	if err != nil {
		klog.V(5).Infof("no storage %s found for pvc %s/%s", *scName, namespace, pvcName)
		return placeHolderCSIDriver, placeHolderHandle
	}

	// We use random prefix to avoid conflict with volume-ids. If PVC is bound in the middle
	// predicate and there is another pod(on same node) that uses same volume then we will overcount
	// the volume and consider both volumes as different.
	volumeHandle := fmt.Sprintf("%s-%s/%s", c.randomVolumeIDPrefix, namespace, pvcName)
	return storageClass.Provisioner, volumeHandle
}
