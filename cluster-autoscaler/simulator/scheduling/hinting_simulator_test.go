/*
Copyright 2022 The Kubernetes Authors.

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

package scheduling

import (
	"testing"
	"time"

	"k8s.io/autoscaler/cluster-autoscaler/simulator/clustersnapshot"
	"k8s.io/autoscaler/cluster-autoscaler/simulator/predicatechecker"
	. "k8s.io/autoscaler/cluster-autoscaler/utils/test"

	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
)

func TestTrySchedulePods(t *testing.T) {
	testCases := []struct {
		desc            string
		nodes           []*apiv1.Node
		pods            []*apiv1.Pod
		newPods         []*apiv1.Pod
		acceptableNodes func(string) bool
		wantErr         bool
	}{
		{
			desc: "two new pods, two nodes",
			nodes: []*apiv1.Node{
				buildReadyNode("n1", 1000, 2000000),
				buildReadyNode("n2", 1000, 2000000),
			},
			pods: []*apiv1.Pod{
				buildScheduledPod("p1", 300, 500000, "n1"),
			},
			newPods: []*apiv1.Pod{
				BuildTestPod("p2", 600, 500000),
				BuildTestPod("p3", 500, 500000),
			},
			acceptableNodes: ScheduleAnywhere,
		},
		{
			desc: "three new pods, two nodes, no fit",
			nodes: []*apiv1.Node{
				buildReadyNode("n1", 1000, 2000000),
				buildReadyNode("n2", 1000, 2000000),
			},
			pods: []*apiv1.Pod{
				buildScheduledPod("p1", 300, 500000, "n1"),
			},
			newPods: []*apiv1.Pod{
				BuildTestPod("p2", 600, 500000),
				BuildTestPod("p3", 500, 500000),
				BuildTestPod("p4", 700, 500000),
			},
			acceptableNodes: ScheduleAnywhere,
			wantErr:         true,
		},
		{
			desc: "no new pods, two nodes",
			nodes: []*apiv1.Node{
				buildReadyNode("n1", 1000, 2000000),
				buildReadyNode("n2", 1000, 2000000),
			},
			pods: []*apiv1.Pod{
				buildScheduledPod("p1", 300, 500000, "n1"),
			},
			newPods:         []*apiv1.Pod{},
			acceptableNodes: ScheduleAnywhere,
		},
		{
			desc: "two nodes, but only one acceptable",
			nodes: []*apiv1.Node{
				buildReadyNode("n1", 1000, 2000000),
				buildReadyNode("n2", 1000, 2000000),
			},
			pods: []*apiv1.Pod{
				buildScheduledPod("p1", 300, 500000, "n1"),
			},
			newPods: []*apiv1.Pod{
				BuildTestPod("p2", 500, 500000),
				BuildTestPod("p3", 500, 500000),
			},
			acceptableNodes: singleNodeOk("n2"),
		},
		{
			desc: "two nodes, but only one acceptable, no fit",
			nodes: []*apiv1.Node{
				buildReadyNode("n1", 1000, 2000000),
				buildReadyNode("n2", 1000, 2000000),
			},
			pods: []*apiv1.Pod{
				buildScheduledPod("p1", 300, 500000, "n1"),
			},
			newPods: []*apiv1.Pod{
				BuildTestPod("p2", 500, 500000),
				BuildTestPod("p3", 500, 500000),
			},
			acceptableNodes: singleNodeOk("n1"),
			wantErr:         true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			clusterSnapshot := clustersnapshot.NewBasicClusterSnapshot()
			predicateChecker, err := predicatechecker.NewTestPredicateChecker()
			assert.NoError(t, err)
			clustersnapshot.InitializeClusterSnapshotOrDie(t, clusterSnapshot, tc.nodes, tc.pods)
			s := NewHintingSimulator(predicateChecker)
			_, err = s.TrySchedulePods(clusterSnapshot, tc.newPods, tc.acceptableNodes)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				numScheduled := countPods(t, clusterSnapshot)
				assert.Equal(t, len(tc.pods)+len(tc.newPods), numScheduled)
				s.DropOldHints()
				// Check if new hints match actually scheduled node names.
				for _, np := range tc.newPods {
					hintedNode, found := s.hints.Get(HintKeyFromPod(np))
					assert.True(t, found)
					actualNode := nodeNameForPod(t, clusterSnapshot, np.Name)
					assert.Equal(t, hintedNode, actualNode)
				}
			}
		})
	}
}

func TestPodSchedulesOnHintedNode(t *testing.T) {
	testCases := []struct {
		desc      string
		nodeNames []string
		podNodes  map[string]string
	}{
		{
			desc:      "single hint",
			nodeNames: []string{"n1", "n2", "n3"},
			podNodes:  map[string]string{"p1": "n2"},
		},
		{
			desc:      "all on one node",
			nodeNames: []string{"n1", "n2", "n3"},
			podNodes: map[string]string{
				"p1": "n2",
				"p2": "n2",
				"p3": "n2",
			},
		},
		{
			desc:      "spread across nodes",
			nodeNames: []string{"n1", "n2", "n3"},
			podNodes: map[string]string{
				"p1": "n1",
				"p2": "n2",
				"p3": "n3",
			},
		},
		{
			desc:      "lots of pods",
			nodeNames: []string{"n1", "n2", "n3"},
			podNodes: map[string]string{
				"p1": "n1",
				"p2": "n1",
				"p3": "n1",
				"p4": "n2",
				"p5": "n2",
				"p6": "n2",
				"p7": "n3",
				"p8": "n3",
				"p9": "n3",
			},
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			clusterSnapshot := clustersnapshot.NewBasicClusterSnapshot()
			predicateChecker, err := predicatechecker.NewTestPredicateChecker()
			assert.NoError(t, err)
			nodes := make([]*apiv1.Node, 0, len(tc.nodeNames))
			for _, n := range tc.nodeNames {
				nodes = append(nodes, buildReadyNode(n, 9999, 9999))
			}
			clustersnapshot.InitializeClusterSnapshotOrDie(t, clusterSnapshot, nodes, []*apiv1.Pod{})
			pods := make([]*apiv1.Pod, 0, len(tc.podNodes))
			s := NewHintingSimulator(predicateChecker)
			for p, n := range tc.podNodes {
				pod := BuildTestPod(p, 1, 1)
				pods = append(pods, pod)
				s.hints.Set(HintKeyFromPod(pod), n)
			}
			_, err = s.TrySchedulePods(clusterSnapshot, pods, ScheduleAnywhere)
			assert.NoError(t, err)

			for p, hinted := range tc.podNodes {
				actual := nodeNameForPod(t, clusterSnapshot, p)
				assert.Equal(t, hinted, actual)
			}
		})
	}
}

func buildReadyNode(name string, cpu, mem int64) *apiv1.Node {
	n := BuildTestNode(name, cpu, mem)
	SetNodeReadyState(n, true, time.Time{})
	return n
}

func buildScheduledPod(name string, cpu, mem int64, nodeName string) *apiv1.Pod {
	p := BuildTestPod(name, cpu, mem)
	p.Spec.NodeName = nodeName
	return p
}

func countPods(t *testing.T, clusterSnapshot clustersnapshot.ClusterSnapshot) int {
	t.Helper()
	count := 0
	nis, err := clusterSnapshot.NodeInfos().List()
	assert.NoError(t, err)
	for _, ni := range nis {
		count += len(ni.Pods)
	}
	return count
}

func nodeNameForPod(t *testing.T, clusterSnapshot clustersnapshot.ClusterSnapshot, pod string) string {
	t.Helper()
	nis, err := clusterSnapshot.NodeInfos().List()
	assert.NoError(t, err)
	for _, ni := range nis {
		for _, pi := range ni.Pods {
			if pi.Pod.Name == pod {
				return ni.Node().Name
			}
		}
	}
	return ""
}

func singleNodeOk(nodeName string) func(string) bool {
	return func(otherNodeName string) bool {
		return nodeName == otherNodeName
	}
}
