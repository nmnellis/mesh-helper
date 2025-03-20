package domain

import (
	"fmt"
	"sort"
)

// Node represents a workload in the dependency tree
type Node struct {
	Name       string
	Children   map[string]*Node
	Metadata   *Metadata
	IsCircular bool
}

type Metadata struct {
	Name      string
	Namespace string
	Identity  string
	Cluster   string
}

// NewNode creates a new node
func NewNode(name string, metadata *Metadata) *Node {
	return &Node{
		Name:     name,
		Metadata: metadata,
		Children: make(map[string]*Node),
	}
}

// AddChild adds a child node to the current node
func (n *Node) AddChild(name string, metadata *Metadata) *Node {
	if _, exists := n.Children[name]; !exists {
		n.Children[name] = NewNode(name, metadata)
	}
	return n.Children[name]
}

func BuildTree(workload *Node, parentNode *Node, sourceToDestMap map[string][]*Metadata, path map[string]bool) {
	// Check for circular dependencies
	if path[workload.Name] {
		// Add the circular reference as a child but don't recurse
		circularNode := parentNode.AddChild(workload.Name, workload.Metadata)
		circularNode.IsCircular = true
		return
	}

	// Mark this workload as part of the current path
	path[workload.Name] = true

	// Add the current workload as a child of the parent
	currentNode := parentNode.AddChild(workload.Name, workload.Metadata)

	// Add all destinations as children
	for _, dest := range sourceToDestMap[workload.Name] {
		// Clone the path for this branch
		newPath := make(map[string]bool)
		for k, v := range path {
			newPath[k] = v
		}

		destNode := &Node{
			Name:       dest.Name,
			Metadata:   dest,
			IsCircular: false,
		}

		BuildTree(destNode, currentNode, sourceToDestMap, newPath)
	}

	// Remove this workload from the path when backtracking
	delete(path, workload.Name)
}

// PrintTree prints the tree structure
func PrintTree(node *Node, prefix string, isLast bool, path map[string]bool) {
	// Check if we've already seen this node in the current path (circular dependency)
	if node.Name != "ROOT" && path[node.Name] {
		if isLast {
			fmt.Printf("%s└── %s (CIRCULAR DEPENDENCY)\n", prefix, node.Name)
		} else {
			fmt.Printf("%s├── %s (CIRCULAR DEPENDENCY)\n", prefix, node.Name)
		}
		return
	}

	// Print current node
	if node.Name == "ROOT" {
		fmt.Println(".")
	} else {
		if isLast {
			fmt.Printf("%s└── %s\n", prefix, node.Name)
			prefix += "    "
		} else {
			fmt.Printf("%s├── %s\n", prefix, node.Name)
			prefix += "│   "
		}
	}

	// Add current node to path
	if node.Name != "ROOT" {
		path[node.Name] = true
	}

	// Get sorted children names for consistent output
	var childNames []string
	for name := range node.Children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)

	// Print children
	for i, name := range childNames {
		isLastChild := i == len(childNames)-1
		// Create a new path map to prevent siblings from being marked as circular
		newPath := make(map[string]bool)
		for k, v := range path {
			newPath[k] = v
		}
		PrintTree(node.Children[name], prefix, isLastChild, newPath)
	}

	// Remove current node from path when backtracking
	if node.Name != "ROOT" {
		delete(path, node.Name)
	}
}
