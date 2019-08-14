package operationexecutor

import (
	"fmt"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/features"
	kevents "k8s.io/kubernetes/pkg/kubelet/events"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/util"
)

// VolumeOperationInterface defines functions which can be used to separate out interaction between operation_generator
// and volume plugins. This allows us to replace the functions which make external RPC calls or call to
// cloudprovider APIs.
type VolumeOperationInterface interface {
	NodeExpandVolume(VolumeToMount, volume.NodeResizeOptions) (bool, error)
}

// NewVolumeOperationInterace returns a new instance of VolumeOperationInterface
func NewVolumeOperationInterace(
	c clientset.Interface,
	mgr *volume.VolumePluginMgr,
	recorder record.EventRecorder) VolumeOperationInterface {
	return &volumeOperationImpl{
		kubeClient:      c,
		volumePluginMgr: mgr,
		recorder:        recorder,
	}
}

type volumeOperationImpl struct {
	kubeClient clientset.Interface

	// volumePluginMgr is the volume plugin manager used to create volume
	// plugin objects.
	volumePluginMgr *volume.VolumePluginMgr

	// recorder is used to record events in the API server
	recorder record.EventRecorder
}

func (volumeOperation *volumeOperationImpl) NodeExpandVolume(volumeToMount VolumeToMount, rsOpts volume.NodeResizeOptions) (bool, error) {
	if !utilfeature.DefaultFeatureGate.Enabled(features.ExpandPersistentVolumes) {
		klog.V(4).Infof("Resizing is not enabled for this volume %s", volumeToMount.VolumeName)
		return true, nil
	}

	if volumeToMount.VolumeSpec != nil &&
		volumeToMount.VolumeSpec.InlineVolumeSpecForCSIMigration {
		klog.V(4).Infof("This volume %s is a migrated inline volume and is not resizable", volumeToMount.VolumeName)
		return true, nil
	}

	// Get expander, if possible
	expandableVolumePlugin, _ :=
		volumeOperation.volumePluginMgr.FindNodeExpandablePluginBySpec(volumeToMount.VolumeSpec)

	if expandableVolumePlugin != nil &&
		expandableVolumePlugin.RequiresFSResize() &&
		volumeToMount.VolumeSpec.PersistentVolume != nil {
		pv := volumeToMount.VolumeSpec.PersistentVolume
		pvc, err := volumeOperation.kubeClient.CoreV1().PersistentVolumeClaims(pv.Spec.ClaimRef.Namespace).Get(pv.Spec.ClaimRef.Name, metav1.GetOptions{})
		if err != nil {
			// Return error rather than leave the file system un-resized, caller will log and retry
			return false, fmt.Errorf("MountVolume.NodeExpandVolume get PVC failed : %v", err)
		}

		pvcStatusCap := pvc.Status.Capacity[v1.ResourceStorage]
		pvSpecCap := pv.Spec.Capacity[v1.ResourceStorage]
		if pvcStatusCap.Cmp(pvSpecCap) < 0 {
			// File system resize was requested, proceed
			klog.V(4).Infof(volumeToMount.GenerateMsgDetailed("MountVolume.NodeExpandVolume entering", fmt.Sprintf("DevicePath %q", volumeToMount.DevicePath)))

			if volumeToMount.VolumeSpec.ReadOnly {
				simpleMsg, detailedMsg := volumeToMount.GenerateMsg("MountVolume.NodeExpandVolume failed", "requested read-only file system")
				klog.Warningf(detailedMsg)
				volumeOperation.recorder.Eventf(volumeToMount.Pod, v1.EventTypeWarning, kevents.FileSystemResizeFailed, simpleMsg)
				return true, nil
			}
			rsOpts.VolumeSpec = volumeToMount.VolumeSpec
			rsOpts.NewSize = pvSpecCap
			rsOpts.OldSize = pvcStatusCap
			resizeDone, resizeErr := expandableVolumePlugin.NodeExpand(rsOpts)
			if resizeErr != nil {
				return false, fmt.Errorf("MountVolume.NodeExpandVolume failed : %v", resizeErr)
			}
			// Volume resizing is not done but it did not error out. This could happen if a CSI volume
			// does not have node stage_unstage capability but was asked to resize the volume before
			// node publish. In which case - we must retry resizing after node publish.
			if !resizeDone {
				return false, nil
			}
			simpleMsg, detailedMsg := volumeToMount.GenerateMsg("MountVolume.NodeExpandVolume succeeded", "")
			volumeOperation.recorder.Eventf(volumeToMount.Pod, v1.EventTypeNormal, kevents.FileSystemResizeSuccess, simpleMsg)
			klog.Infof(detailedMsg)
			// File system resize succeeded, now update the PVC's Capacity to match the PV's
			err = util.MarkFSResizeFinished(pvc, pvSpecCap, volumeOperation.kubeClient)
			if err != nil {
				// On retry, NodeExpandVolume will be called again but do nothing
				return false, fmt.Errorf("MountVolume.NodeExpandVolume update PVC status failed : %v", err)
			}
			return true, nil
		}
	}
	return true, nil
}
