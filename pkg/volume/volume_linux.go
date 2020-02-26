// +build linux

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

package volume

import (
	"path/filepath"
	"syscall"

	"os"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/features"
)

const (
	rwMask   = os.FileMode(0660)
	roMask   = os.FileMode(0440)
	execMask = os.FileMode(0110)
)

// SetVolumeOwnership modifies the given volume to be owned by
// fsGroup, and sets SetGid so that newly created files are owned by
// fsGroup. If fsGroup is nil nothing is done.
func SetVolumeOwnership(mounter Mounter, fsGroup *int64, fsGroupChangePolicy *v1.PodFSGroupChangePolicy) error {
	if fsGroup == nil {
		return nil
	}

	if skipPermissionChange(mounter, fsGroup, fsGroupChangePolicy) {
		return nil
	}

	klog.Warningf("Setting volume ownership for %s and fsGroup set. If the volume has a lot of files then setting volume ownership could be slow, see https://github.com/kubernetes/kubernetes/issues/69699", mounter.GetPath())

	rootDir := mounter.GetPath()
	names, err := readDirNames(rootDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		filename := filepath.Join(rootDir, name)
		err1 := changeAllFilesPermissions(filename, fsGroup, mounter.GetAttributes().ReadOnly)
		if err1 != nil {
			return err
		}
	}
	_, err = changeFilePermission(rootDir, fsGroup, mounter.GetAttributes().ReadOnly)
	return err
}

func changeAllFilesPermissions(path string, fsGroup *int64, readonly bool) error {
	fsInfo, err := changeFilePermission(path, fsGroup, readonly)
	if err != nil {
		return err
	}
	if !fsInfo.IsDir() {
		return nil
	}

	names, err := readDirNames(path)
	if err != nil {
		return err
	}

	for _, name := range names {
		filename := filepath.Join(path, name)
		err1 := changeAllFilesPermissions(filename, fsGroup, readonly)
		if err1 != nil {
			return err
		}
	}
	return nil
}

func changeFilePermission(filename string, fsGroup *int64, readonly bool) (os.FileInfo, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return info, err
	}
	// chown and chmod pass through to the underlying file for symlinks.
	// Symlinks have a mode of 777 but this really doesn't mean anything.
	// The permissions of the underlying file are what matter.
	// However, if one reads the mode of a symlink then chmods the symlink
	// with that mode, it changes the mode of the underlying file, overridden
	// the defaultMode and permissions initialized by the volume plugin, which
	// is not what we want; thus, we skip chown/chmod for symlinks.
	if info.Mode()&os.ModeSymlink != 0 {
		return info, nil
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info, nil
	}

	if stat == nil {
		klog.Errorf("Got nil stat_t for path %v while setting ownership of volume", filename)
		return info, nil
	}

	err = os.Chown(filename, int(stat.Uid), int(*fsGroup))
	if err != nil {
		klog.Errorf("Chown failed on %v: %v", filename, err)
	}

	mask := rwMask
	if readonly {
		mask = roMask
	}

	if info.IsDir() {
		mask |= os.ModeSetgid
		mask |= execMask
	}

	err = os.Chmod(filename, info.Mode()|mask)
	if err != nil {
		klog.Errorf("Chmod failed on %v: %v", filename, err)
	}

	return info, nil
}

func skipPermissionChange(mounter Mounter, fsGroup *int64, fsGroupChangePolicy *v1.PodFSGroupChangePolicy) bool {
	if !utilfeature.DefaultFeatureGate.Enabled(features.ConfigurableFSGroupPermissions) {
		klog.V(4).Infof("perform recursive ownership change, configurable fsGroupChangepolicy is not enabled")
		return false
	}
	if fsGroupChangePolicy == nil || *fsGroupChangePolicy != v1.OnRootMismatch {
		klog.V(4).Infof("perform recursive ownership change, fsGroupChangePolicy is set to %v", fsGroupChangePolicy)
		return false
	}
	return !requiresPermissionChange(mounter.GetPath(), fsGroup, mounter.GetAttributes().ReadOnly)
}

func requiresPermissionChange(rootDir string, fsGroup *int64, readonly bool) bool {
	fsInfo, err := os.Stat(rootDir)
	if err != nil {
		klog.Errorf("performing recursive ownership change on %s because reading permissions of root volume failed: %v", rootDir, err)
		return true
	}
	stat, ok := fsInfo.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		klog.Errorf("performing recursive ownership change on %s because reading permissions of root volume failed", rootDir)
		return true
	}

	if int(stat.Gid) != int(*fsGroup) {
		klog.V(4).Infof("expected group ownership of volume %s did not match with: %d", rootDir, stat.Gid)
		return true
	}
	unixPerms := rwMask

	if readonly {
		unixPerms = roMask
	}

	// if rootDir is not a directory then we should apply permission change anyways
	if !fsInfo.IsDir() {
		return true
	}
	unixPerms |= execMask

	if unixPerms != fsInfo.Mode().Perm() || (fsInfo.Mode()&os.ModeSetgid == 0) {
		klog.V(4).Infof("performing recursive ownership change on %s because of mismatching mode", rootDir)
		return true
	}
	return false
}

// readDirNames reads the directory named by dirname and returns
// a list of directory entries.
func readDirNames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	return names, nil
}
