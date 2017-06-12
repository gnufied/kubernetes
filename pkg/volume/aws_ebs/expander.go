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

package aws_ebs

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/aws"
	"k8s.io/kubernetes/pkg/volume"
)

type awsElasticBlockStoreExpander struct {
	host       volume.VolumeHost
	awsVolumes aws.Volumes
}

var _ volume.Expander = &awsElasticBlockStoreExpander{}
var _ volume.ExpandableVolumePlugin = &awsElasticBlockStorePlugin{}

func (plugin *awsElasticBlockStorePlugin) NewExpander() (volume.Expander, error) {
	awsCloud, err := getCloudProvider(plugin.host.GetCloudProvider())
	if err != nil {
		return nil, err
	}

	return &awsElasticBlockStoreExpander{
		host:       plugin.host,
		awsVolumes: awsCloud,
	}, nil
}

func (expander *awsElasticBlockStoreExpander) ExpandVolumeDevice(
	pv *v1.PersistentVolume,
	newSize resource.Quantity,
	oldSize resource.Quantity) error {
	glog.Infof("Hello World")
	return nil
}
