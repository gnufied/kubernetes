/*
Copyright 2019 The Kubernetes Authors.

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

package csi

import (
	"fmt"
	"io"
	"testing"

	csipbv1 "github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/kubernetes/pkg/volume/csi/fake"
)

func TestGetMetrics(t *testing.T) {
	tests := []struct {
		name          string
		volumeID      string
		targetPath    string
		expectSuccess bool
	}{
		{
			name:          "with valid name and volume id",
			expectSuccess: true,
			volumeID:      "foobar",
			targetPath:    "/mnt/foo",
		},
	}

	for _, tc := range tests {
		metricsGetter := &metricsCsi{volumeID: tc.volumeID, targetPath: tc.targetPath}
		metricsGetter.csiClient = &csiDriverClient{
			driverName: "com.google.gcepd",
			nodeV1ClientCreator: func(addr csiAddr) (csipbv1.NodeClient, io.Closer, error) {
				nodeClient := fake.NewNodeClientWithVolumeStats(true /* VolumeStatsCapable */)
				fakeCloser := fake.NewCloser(t)
				nodeClient.SetNodeVolumeStatsResp(getRawVolumeInfo())
				return nodeClient, fakeCloser, nil
			},
		}
		metrics, err := metricsGetter.GetMetrics()
		if err != nil {
			t.Fatalf("unexpected error : %v", err)
		}
		if metrics == nil {
			t.Errorf("unexpected nil metrics")
		}
		fmt.Printf("metrics : %+v", metrics)

	}
}

func getRawVolumeInfo() *csipbv1.NodeGetVolumeStatsResponse {
	return &csipbv1.NodeGetVolumeStatsResponse{
		Usage: []*csipbv1.VolumeUsage{
			&csipbv1.VolumeUsage{
				Available: int64(10),
				Total:     int64(10),
				Used:      int64(2),
				Unit:      csipbv1.VolumeUsage_BYTES,
			},
		},
	}
}
