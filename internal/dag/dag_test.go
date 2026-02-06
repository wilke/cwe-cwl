package dag

import (
	"testing"
)

func TestNewDAG(t *testing.T) {
	dag := NewDAG("test-dag", "test-workflow")

	if dag.ID != "test-dag" {
		t.Errorf("Expected ID 'test-dag', got '%s'", dag.ID)
	}
	if dag.WorkflowID != "test-workflow" {
		t.Errorf("Expected WorkflowID 'test-workflow', got '%s'", dag.WorkflowID)
	}
	if len(dag.Nodes) != 0 {
		t.Errorf("Expected empty nodes, got %d", len(dag.Nodes))
	}
}

func TestDAG_AddNode(t *testing.T) {
	dag := NewDAG("test", "wf")

	node := &Node{
		ID:     "node1",
		StepID: "step1",
		Status: StatusPending,
	}

	dag.AddNode(node)

	if len(dag.Nodes) != 1 {
		t.Errorf("Expected 1 node, got %d", len(dag.Nodes))
	}

	retrieved := dag.GetNode("node1")
	if retrieved == nil {
		t.Error("Expected to find node1")
	}
	if retrieved.ID != "node1" {
		t.Errorf("Expected ID 'node1', got '%s'", retrieved.ID)
	}
}

func TestDAG_GetReadyNodes(t *testing.T) {
	dag := NewDAG("test", "wf")

	dag.AddNode(&Node{ID: "n1", Status: StatusPending})
	dag.AddNode(&Node{ID: "n2", Status: StatusReady})
	dag.AddNode(&Node{ID: "n3", Status: StatusRunning})
	dag.AddNode(&Node{ID: "n4", Status: StatusReady})

	ready := dag.GetReadyNodes()

	if len(ready) != 2 {
		t.Errorf("Expected 2 ready nodes, got %d", len(ready))
	}

	readyIDs := make(map[string]bool)
	for _, n := range ready {
		readyIDs[n.ID] = true
	}
	if !readyIDs["n2"] || !readyIDs["n4"] {
		t.Errorf("Expected n2 and n4 to be ready, got %v", readyIDs)
	}
}

func TestDAG_UpdateNodeStatus(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Create a simple chain: n1 -> n2 -> n3
	n1 := &Node{ID: "n1", Status: StatusPending, Dependents: []string{"n2"}}
	n2 := &Node{ID: "n2", Status: StatusPending, Dependencies: []string{"n1"}, Dependents: []string{"n3"}}
	n3 := &Node{ID: "n3", Status: StatusPending, Dependencies: []string{"n2"}}

	dag.AddNode(n1)
	dag.AddNode(n2)
	dag.AddNode(n3)

	// Complete n1
	err := dag.UpdateNodeStatus("n1", StatusCompleted)
	if err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	// n2 should now be ready
	if n2.GetStatus() != StatusReady {
		t.Errorf("Expected n2 to be ready after n1 completed, got %s", n2.GetStatus())
	}

	// n3 should still be pending
	if n3.GetStatus() != StatusPending {
		t.Errorf("Expected n3 to still be pending, got %s", n3.GetStatus())
	}
}

func TestDAG_UpdateNodeStatus_Failure(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Create a chain: n1 -> n2 -> n3
	n1 := &Node{ID: "n1", Status: StatusPending, Dependents: []string{"n2"}}
	n2 := &Node{ID: "n2", Status: StatusPending, Dependencies: []string{"n1"}, Dependents: []string{"n3"}}
	n3 := &Node{ID: "n3", Status: StatusPending, Dependencies: []string{"n2"}}

	dag.AddNode(n1)
	dag.AddNode(n2)
	dag.AddNode(n3)

	// Fail n1
	err := dag.UpdateNodeStatus("n1", StatusFailed)
	if err != nil {
		t.Fatalf("Failed to update status: %v", err)
	}

	// n2 and n3 should be skipped
	if n2.GetStatus() != StatusSkipped {
		t.Errorf("Expected n2 to be skipped, got %s", n2.GetStatus())
	}
	if n3.GetStatus() != StatusSkipped {
		t.Errorf("Expected n3 to be skipped, got %s", n3.GetStatus())
	}
}

func TestDAG_IsComplete(t *testing.T) {
	dag := NewDAG("test", "wf")

	dag.AddNode(&Node{ID: "n1", Status: StatusPending})
	dag.AddNode(&Node{ID: "n2", Status: StatusCompleted})

	if dag.IsComplete() {
		t.Error("Expected incomplete DAG")
	}

	dag.Nodes["n1"].SetStatus(StatusCompleted)

	if !dag.IsComplete() {
		t.Error("Expected complete DAG")
	}
}

func TestDAG_HasFailed(t *testing.T) {
	dag := NewDAG("test", "wf")

	dag.AddNode(&Node{ID: "n1", Status: StatusCompleted})
	dag.AddNode(&Node{ID: "n2", Status: StatusCompleted})

	if dag.HasFailed() {
		t.Error("Expected no failures")
	}

	dag.Nodes["n2"].SetStatus(StatusFailed)

	if !dag.HasFailed() {
		t.Error("Expected failure")
	}
}

func TestDAG_TopoSort(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Diamond pattern: n1 -> n2 -> n4
	//                  n1 -> n3 -> n4
	n1 := &Node{ID: "n1", Dependents: []string{"n2", "n3"}}
	n2 := &Node{ID: "n2", Dependencies: []string{"n1"}, Dependents: []string{"n4"}}
	n3 := &Node{ID: "n3", Dependencies: []string{"n1"}, Dependents: []string{"n4"}}
	n4 := &Node{ID: "n4", Dependencies: []string{"n2", "n3"}}

	dag.AddNode(n1)
	dag.AddNode(n2)
	dag.AddNode(n3)
	dag.AddNode(n4)

	sorted, err := dag.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort failed: %v", err)
	}

	if len(sorted) != 4 {
		t.Errorf("Expected 4 nodes in sorted order, got %d", len(sorted))
	}

	// n1 must come before n2 and n3
	// n2 and n3 must come before n4
	positions := make(map[string]int)
	for i, id := range sorted {
		positions[id] = i
	}

	if positions["n1"] > positions["n2"] {
		t.Error("n1 should come before n2")
	}
	if positions["n1"] > positions["n3"] {
		t.Error("n1 should come before n3")
	}
	if positions["n2"] > positions["n4"] {
		t.Error("n2 should come before n4")
	}
	if positions["n3"] > positions["n4"] {
		t.Error("n3 should come before n4")
	}
}

func TestDAG_TopoSort_Cycle(t *testing.T) {
	dag := NewDAG("test", "wf")

	// Create cycle: n1 -> n2 -> n1
	n1 := &Node{ID: "n1", Dependencies: []string{"n2"}, Dependents: []string{"n2"}}
	n2 := &Node{ID: "n2", Dependencies: []string{"n1"}, Dependents: []string{"n1"}}

	dag.AddNode(n1)
	dag.AddNode(n2)

	_, err := dag.TopoSort()
	if err == nil {
		t.Error("Expected cycle detection error")
	}
}

func TestDAG_GetStats(t *testing.T) {
	dag := NewDAG("test", "wf")

	dag.AddNode(&Node{ID: "n1", Status: StatusPending})
	dag.AddNode(&Node{ID: "n2", Status: StatusReady})
	dag.AddNode(&Node{ID: "n3", Status: StatusRunning})
	dag.AddNode(&Node{ID: "n4", Status: StatusCompleted})
	dag.AddNode(&Node{ID: "n5", Status: StatusFailed})
	dag.AddNode(&Node{ID: "n6", Status: StatusSkipped})

	stats := dag.GetStats()

	if stats[StatusPending] != 1 {
		t.Errorf("Expected 1 pending, got %d", stats[StatusPending])
	}
	if stats[StatusReady] != 1 {
		t.Errorf("Expected 1 ready, got %d", stats[StatusReady])
	}
	if stats[StatusRunning] != 1 {
		t.Errorf("Expected 1 running, got %d", stats[StatusRunning])
	}
	if stats[StatusCompleted] != 1 {
		t.Errorf("Expected 1 completed, got %d", stats[StatusCompleted])
	}
	if stats[StatusFailed] != 1 {
		t.Errorf("Expected 1 failed, got %d", stats[StatusFailed])
	}
	if stats[StatusSkipped] != 1 {
		t.Errorf("Expected 1 skipped, got %d", stats[StatusSkipped])
	}
}

func TestDAG_InitializeReadyNodes(t *testing.T) {
	dag := NewDAG("test", "wf")

	// n1 has no dependencies, n2 depends on n1
	n1 := &Node{ID: "n1", Status: StatusPending, Dependencies: []string{}, Dependents: []string{"n2"}}
	n2 := &Node{ID: "n2", Status: StatusPending, Dependencies: []string{"n1"}, Dependents: []string{}}

	dag.AddNode(n1)
	dag.AddNode(n2)

	dag.InitializeReadyNodes()

	if n1.GetStatus() != StatusReady {
		t.Errorf("Expected n1 to be ready, got %s", n1.GetStatus())
	}
	if n2.GetStatus() != StatusPending {
		t.Errorf("Expected n2 to still be pending, got %s", n2.GetStatus())
	}
	if len(dag.InputNodes) != 1 || dag.InputNodes[0] != "n1" {
		t.Errorf("Expected InputNodes to be [n1], got %v", dag.InputNodes)
	}
}

func TestNode_IsScattered(t *testing.T) {
	node1 := &Node{ID: "n1", ScatterIndex: nil}
	if node1.IsScattered() {
		t.Error("Expected non-scattered node")
	}

	node2 := &Node{ID: "n2", ScatterIndex: []int{0}}
	if !node2.IsScattered() {
		t.Error("Expected scattered node")
	}
}

func TestNode_GetScatterIndexString(t *testing.T) {
	node1 := &Node{ID: "n1", ScatterIndex: nil}
	if node1.GetScatterIndexString() != "" {
		t.Errorf("Expected empty string for non-scattered node")
	}

	node2 := &Node{ID: "n2", ScatterIndex: []int{1, 2, 3}}
	expected := "1_2_3"
	if node2.GetScatterIndexString() != expected {
		t.Errorf("Expected '%s', got '%s'", expected, node2.GetScatterIndexString())
	}
}

func TestGenerateNodeID(t *testing.T) {
	testCases := []struct {
		stepID       string
		scatterIndex []int
		expected     string
	}{
		{"step1", nil, "step1"},
		{"step1", []int{0}, "step1_0"},
		{"step1", []int{1, 2}, "step1_1_2"},
	}

	for _, tc := range testCases {
		result := GenerateNodeID(tc.stepID, tc.scatterIndex)
		if result != tc.expected {
			t.Errorf("Expected '%s', got '%s'", tc.expected, result)
		}
	}
}

func TestDAG_GetCompletedOutputs(t *testing.T) {
	dag := NewDAG("test", "wf")

	n1 := &Node{
		ID:      "n1",
		Status:  StatusCompleted,
		Outputs: map[string]interface{}{"out1": "value1"},
	}
	n2 := &Node{
		ID:      "n2",
		Status:  StatusRunning,
		Outputs: map[string]interface{}{"out2": "value2"},
	}
	n3 := &Node{
		ID:      "n3",
		Status:  StatusCompleted,
		Outputs: map[string]interface{}{"out3": "value3"},
	}

	dag.AddNode(n1)
	dag.AddNode(n2)
	dag.AddNode(n3)

	outputs := dag.GetCompletedOutputs()

	if len(outputs) != 2 {
		t.Errorf("Expected 2 completed outputs, got %d", len(outputs))
	}
	if outputs["n1"]["out1"] != "value1" {
		t.Errorf("Expected n1 output")
	}
	if outputs["n3"]["out3"] != "value3" {
		t.Errorf("Expected n3 output")
	}
	if _, ok := outputs["n2"]; ok {
		t.Error("Did not expect n2 output (not completed)")
	}
}
