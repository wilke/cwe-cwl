// Package dag provides DAG construction and scheduling for CWL workflows.
package dag

import (
	"fmt"
	"sync"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// NodeStatus represents the execution status of a DAG node.
type NodeStatus string

const (
	StatusPending   NodeStatus = "pending"
	StatusReady     NodeStatus = "ready"
	StatusRunning   NodeStatus = "running"
	StatusCompleted NodeStatus = "completed"
	StatusFailed    NodeStatus = "failed"
	StatusSkipped   NodeStatus = "skipped"
)

// Node represents a node in the workflow DAG.
type Node struct {
	ID           string
	StepID       string
	ScatterIndex []int              // nil for non-scattered, index for scattered
	Status       NodeStatus
	Inputs       map[string]interface{}
	Outputs      map[string]interface{}
	Dependencies []string // IDs of nodes this node depends on
	Dependents   []string // IDs of nodes that depend on this node
	Step         *cwl.WorkflowStep
	Tool         *cwl.Document // Resolved tool for this step
	Error        string
	TaskID       string // BV-BRC Task ID when running
	mu           sync.RWMutex
}

// DAG represents a directed acyclic graph of workflow steps.
type DAG struct {
	ID          string
	WorkflowID  string
	Nodes       map[string]*Node
	InputNodes  []string // Nodes that have no dependencies
	OutputNodes []string // Nodes that produce workflow outputs
	mu          sync.RWMutex
}

// NewDAG creates a new DAG.
func NewDAG(id, workflowID string) *DAG {
	return &DAG{
		ID:         id,
		WorkflowID: workflowID,
		Nodes:      make(map[string]*Node),
	}
}

// AddNode adds a node to the DAG.
func (d *DAG) AddNode(node *Node) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Nodes[node.ID] = node
}

// GetNode returns a node by ID.
func (d *DAG) GetNode(id string) *Node {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Nodes[id]
}

// GetReadyNodes returns all nodes that are ready to execute.
func (d *DAG) GetReadyNodes() []*Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var ready []*Node
	for _, node := range d.Nodes {
		if node.GetStatus() == StatusReady {
			ready = append(ready, node)
		}
	}
	return ready
}

// GetPendingNodes returns all nodes that are pending.
func (d *DAG) GetPendingNodes() []*Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var pending []*Node
	for _, node := range d.Nodes {
		if node.GetStatus() == StatusPending {
			pending = append(pending, node)
		}
	}
	return pending
}

// UpdateNodeStatus updates a node's status and checks if dependents become ready.
func (d *DAG) UpdateNodeStatus(nodeID string, status NodeStatus) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	node, ok := d.Nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	node.SetStatus(status)

	// If node completed, check if dependents are now ready
	if status == StatusCompleted {
		for _, depID := range node.Dependents {
			if dep, ok := d.Nodes[depID]; ok {
				if d.areDependenciesSatisfiedLocked(dep) {
					dep.SetStatus(StatusReady)
				}
			}
		}
	}

	// If node failed, mark dependents as skipped
	if status == StatusFailed {
		d.markDependentsSkippedLocked(node)
	}

	return nil
}

// areDependenciesSatisfiedLocked checks if all dependencies are completed.
// Must be called with d.mu held.
func (d *DAG) areDependenciesSatisfiedLocked(node *Node) bool {
	for _, depID := range node.Dependencies {
		dep, ok := d.Nodes[depID]
		if !ok {
			return false
		}
		status := dep.GetStatus()
		if status != StatusCompleted && status != StatusSkipped {
			return false
		}
	}
	return true
}

// markDependentsSkippedLocked marks all dependent nodes as skipped.
// Must be called with d.mu held.
func (d *DAG) markDependentsSkippedLocked(node *Node) {
	for _, depID := range node.Dependents {
		if dep, ok := d.Nodes[depID]; ok {
			status := dep.GetStatus()
			if status == StatusPending || status == StatusReady {
				dep.SetStatus(StatusSkipped)
				d.markDependentsSkippedLocked(dep)
			}
		}
	}
}

// IsComplete returns true if all nodes have finished execution.
func (d *DAG) IsComplete() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, node := range d.Nodes {
		status := node.GetStatus()
		if status == StatusPending || status == StatusReady || status == StatusRunning {
			return false
		}
	}
	return true
}

// HasFailed returns true if any node has failed.
func (d *DAG) HasFailed() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, node := range d.Nodes {
		if node.GetStatus() == StatusFailed {
			return true
		}
	}
	return false
}

// GetCompletedOutputs collects outputs from all completed nodes.
func (d *DAG) GetCompletedOutputs() map[string]map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	outputs := make(map[string]map[string]interface{})
	for id, node := range d.Nodes {
		if node.GetStatus() == StatusCompleted && node.Outputs != nil {
			outputs[id] = node.Outputs
		}
	}
	return outputs
}

// InitializeReadyNodes marks nodes with no dependencies as ready.
func (d *DAG) InitializeReadyNodes() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, node := range d.Nodes {
		if len(node.Dependencies) == 0 && node.GetStatus() == StatusPending {
			node.SetStatus(StatusReady)
			d.InputNodes = append(d.InputNodes, node.ID)
		}
	}
}

// TopoSort returns a topologically sorted list of node IDs.
func (d *DAG) TopoSort() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Kahn's algorithm
	inDegree := make(map[string]int)
	for id, node := range d.Nodes {
		inDegree[id] = len(node.Dependencies)
	}

	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		sorted = append(sorted, nodeID)

		node := d.Nodes[nodeID]
		for _, depID := range node.Dependents {
			inDegree[depID]--
			if inDegree[depID] == 0 {
				queue = append(queue, depID)
			}
		}
	}

	if len(sorted) != len(d.Nodes) {
		return nil, fmt.Errorf("cycle detected in DAG")
	}

	return sorted, nil
}

// GetStats returns execution statistics for the DAG.
func (d *DAG) GetStats() map[NodeStatus]int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := make(map[NodeStatus]int)
	for _, node := range d.Nodes {
		stats[node.GetStatus()]++
	}
	return stats
}

// Node methods

// GetStatus returns the node's current status.
func (n *Node) GetStatus() NodeStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Status
}

// SetStatus sets the node's status.
func (n *Node) SetStatus(status NodeStatus) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Status = status
}

// SetOutputs sets the node's outputs.
func (n *Node) SetOutputs(outputs map[string]interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Outputs = outputs
}

// SetError sets the node's error message.
func (n *Node) SetError(err string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Error = err
}

// SetTaskID sets the BV-BRC task ID.
func (n *Node) SetTaskID(taskID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.TaskID = taskID
}

// GetTaskID returns the BV-BRC task ID.
func (n *Node) GetTaskID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.TaskID
}

// IsScattered returns true if this node is part of a scatter operation.
func (n *Node) IsScattered() bool {
	return n.ScatterIndex != nil
}

// GetScatterIndexString returns the scatter index as a string.
func (n *Node) GetScatterIndexString() string {
	if n.ScatterIndex == nil {
		return ""
	}
	return cwl.IndexToString(n.ScatterIndex)
}

// GenerateNodeID generates a unique node ID.
func GenerateNodeID(stepID string, scatterIndex []int) string {
	if scatterIndex == nil {
		return stepID
	}
	return fmt.Sprintf("%s_%s", stepID, cwl.IndexToString(scatterIndex))
}
