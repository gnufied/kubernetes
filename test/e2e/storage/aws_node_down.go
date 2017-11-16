/*
Copyright 2017 The Kubernetes Authors.

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

package storage

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	awscloud "k8s.io/kubernetes/pkg/cloudprovider/providers/aws"
	"k8s.io/kubernetes/test/e2e/framework"
)

var _ = SIGDescribe("Node Poweroff [Features:aws] [Slow] [Disruptive]", func() {
	var (
		c         clientset.Interface
		ns        string
		ec2Client *ec2.EC2
		err       error
	)

	f := framework.NewDefaultFramework("node-poweroff-aws")
	BeforeEach(func() {
		framework.SkipUnlessProviderIs("aws")
		c = f.ClientSet
		ns = f.Namespace.Name
		framework.ExpectNoError(framework.WaitForAllNodesSchedulable(c, framework.TestContext.NodeSchedulableTimeout))
		nodeList := framework.GetReadySchedulableNodesOrDie(f.ClientSet)
		Expect(nodeList.Items).NotTo(BeEmpty(), "Unable to find ready and schedulable Node")
		Expect(len(nodeList.Items) > 1).To(BeTrue(), "At least 2 nodes are required for this test")

		ec2Client = ec2.New(session.New())
		Expect(ec2Client).NotTo(BeNil(), "Unable to Create aws client")
	})

	It("Verify volume status after node is powered off", func() {
		defaultScName := getDefaultStorageClassName(c)
		verifyDefaultStorageClass(c, defaultScName, true)

		test := storageClassTest{
			name:      "default",
			claimSize: "2Gi",
		}

		pvc := newClaim(test, ns, "default")
		pvc, err = c.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(pvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvc).ToNot(Equal(nil))

		By("Waiting for pvc to be in bound phase")
		pvcClaims := []*v1.PersistentVolumeClaim{pvc}
		pvs, err := framework.WaitForPVClaimBoundPhase(c, pvcClaims, framework.ClaimProvisionTimeout)
		Expect(err).NotTo(HaveOccurred(), "Failed waiting for PVC to be bound %v", err)

		By("Creating a deployment")
		deployment, err := framework.CreateDeployment(c, int32(1), map[string]string{"test": "app"}, ns, pvcClaims, "")
		defer c.Extensions().Deployments(ns).Delete(deployment.Name, &metav1.DeleteOptions{})

		By("Get a pod from deployment")
		podList, err := framework.GetPodsForDeployment(c, deployment)
		Expect(podList.Items).NotTo(BeEmpty())
		pod := podList.Items[0]
		node1 := types.NodeName(pod.Spec.NodeName)

		kid := pvs[0].Spec.PersistentVolumeSource.AWSElasticBlockStore.VolumeID

		volumeID, err := awscloud.GetAWSVolumeID(kid)
		Expect(err).NotTo(HaveOccurred(), "Error converting kube volume id %s to aws volume id with %v", kid, err)

		By("Veryfing if disk is attached")
		ec2Instance, err := findAWSInstanceByNodeName(node1, ec2Client)

		if err != nil {
			framework.Failf("Error fetching node %s from AWS with error : %v", node1, err)
		}

		oldInstaneId := aws.StringValue(ec2Instance.InstanceId)

		err = waitForAWSAttachedState(oldInstaneId, volumeID, ec2Client)
		Expect(err).NotTo(HaveOccurred(), "volume %s to attached to node %s but got %v", volumeID, node1, err)

		By("Shutting down the AWS Node")
		err = shutDownAWSNode(ec2Instance, ec2Client)
		if err != nil {
			framework.Failf("Error while shutting down node %s with %v", node1, err)
		}

		err = waitForAWSInstanceState(node1, ec2Client, "stopped")

		if err != nil {
			framework.Failf("Error while waiting for node %s shutdown with %v", node1, err)
		}

		By("Waiting for pod to fail over to different node")
		node2, err := waitForPodToFailover(c, deployment, node1)
		Expect(err).NotTo(HaveOccurred(), "Pod did not fail over to a different node")

		newInstance, err := findAWSInstanceByNodeName(node2, ec2Client)

		if err != nil {
			framework.Failf("Error fetching node %s from AWS with error : %v", node2, err)
		}

		newInstanceID := aws.StringValue(newInstance.InstanceId)

		By("Waiting for volume to detach from old node")
		err = waitForAWSDetachedState(oldInstaneId, volumeID, ec2Client)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for volume %s to detach from %s", volumeID, node1)

		By("Waiting for volume to attach to new Node")
		err = waitForAWSAttachedState(newInstanceID, volumeID, ec2Client)
		Expect(err).NotTo(HaveOccurred(), "Error waiting for volume %s to attach to %s", volumeID, node2)

		By("Waiting for old node to start")
		err = startAWSNode(ec2Instance, ec2Client)
		Expect(err).NotTo(HaveOccurred(), "Error starting node %s with error : %v", node1, err)

		err = waitForAWSInstanceState(node1, ec2Client, "running")
		Expect(err).NotTo(HaveOccurred(), "Error waiting for node %s to be running with %v", node1, err)
	})
})

func findAWSInstanceByNodeName(nodeName types.NodeName, ec2Client *ec2.EC2) (*ec2.Instance, error) {
	privateDNSName := string(nodeName)
	filter := &ec2.Filter{
		Name: aws.String("private-dns-name"),
	}
	filter.Values = append(filter.Values, aws.String(privateDNSName))

	request := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{filter},
	}
	response, err := ec2Client.DescribeInstances(request)

	if err != nil {
		return nil, err
	}
	results := []*ec2.Instance{}

	for _, reservation := range response.Reservations {
		results = append(results, reservation.Instances...)
	}

	if len(results) > 0 {
		return results[0], nil
	}
	err = fmt.Errorf("Node not found %s", nodeName)
	return nil, err
}

func shutDownAWSNode(instance *ec2.Instance, ec2Client *ec2.EC2) error {
	request := &ec2.StopInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	}
	_, err := ec2Client.StopInstances(request)
	return err
}

func startAWSNode(instance *ec2.Instance, ec2Client *ec2.EC2) error {
	request := &ec2.StartInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	}
	_, err := ec2Client.StartInstances(request)
	return err
}

func waitForAWSInstanceState(nodeName types.NodeName, ec2Client *ec2.EC2, desiredState string) error {
	backoff := wait.Backoff{
		Duration: 10 * time.Second,
		Factor:   1.2,
		Steps:    21,
	}
	return wait.ExponentialBackoff(backoff, func() (bool, error) {
		node, err := findAWSInstanceByNodeName(nodeName, ec2Client)

		if err != nil {
			framework.Logf("Error fetching node %s with %v", nodeName, err)
			return false, err
		}

		instanceState := aws.StringValue(node.State.Name)

		if instanceState == desiredState {
			return true, nil
		}
		return false, nil
	})
}

func waitForAWSAttachedState(instanceId string, diskName string, ec2Client *ec2.EC2) error {
	return wait.PollImmediate(2*time.Second, 21*time.Minute, func() (bool, error) {
		ebsDisk, err := getEBSVolume(diskName, ec2Client)
		if err != nil {
			framework.Logf("Error describe disk %s from node %s with %v", diskName, instanceId, err)
			return false, err
		}

		diskState := aws.StringValue(ebsDisk.State)
		// If disk is not in-use it is not attached
		if diskState != ec2.VolumeStateInUse {
			return false, nil
		}

		attachments := ebsDisk.Attachments
		if len(attachments) > 0 {
			attachment := attachments[0]
			attachmentState := aws.StringValue(attachment.State)
			attachedInstanceID := aws.StringValue(attachment.InstanceId)

			if attachedInstanceID == instanceId &&
				attachmentState == ec2.VolumeAttachmentStateAttached {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForAWSDetachedState(instanceId string, diskName string, ec2Client *ec2.EC2) error {
	return wait.PollImmediate(2*time.Second, 21*time.Minute, func() (bool, error) {
		ebsDisk, err := getEBSVolume(diskName, ec2Client)
		if err != nil {
			framework.Logf("Error describe disk %s from node %s with %v", diskName, instanceId, err)
			return false, err
		}

		diskState := aws.StringValue(ebsDisk.State)
		// If disk is available it is detached
		if diskState == ec2.VolumeStateAvailable {
			return true, nil
		}

		attachments := ebsDisk.Attachments
		if len(attachments) > 0 {
			attachment := attachments[0]
			attachedInstanceID := aws.StringValue(attachment.InstanceId)

			if attachedInstanceID != instanceId {
				return true, nil
			}
		}
		return false, nil
	})
}

func getEBSVolume(diskName string, ec2Client *ec2.EC2) (*ec2.Volume, error) {
	results := []*ec2.Volume{}

	request := &ec2.DescribeVolumesInput{
		VolumeIds: []*string{aws.String(diskName)},
	}
	response, err := ec2Client.DescribeVolumes(request)

	if err != nil {
		framework.Logf("Error describe disk %s with %v", diskName, err)
		return nil, err
	}

	results = append(results, response.Volumes...)

	if len(results) > 0 {
		return results[0], nil
	}
	return nil, fmt.Errorf("Disk %s is not found", diskName)
}
