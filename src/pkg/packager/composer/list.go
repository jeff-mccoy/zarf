// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package composer contains functions for composing components within Zarf packages.
package composer

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/defenseunicorns/zarf/src/internal/packager/validate"
	"github.com/defenseunicorns/zarf/src/pkg/layout"
	"github.com/defenseunicorns/zarf/src/pkg/oci"
	"github.com/defenseunicorns/zarf/src/pkg/packager/deprecated"
	"github.com/defenseunicorns/zarf/src/pkg/utils"
	"github.com/defenseunicorns/zarf/src/pkg/utils/helpers"
	"github.com/defenseunicorns/zarf/src/types"
)

// Node is a node in the import chain
type Node struct {
	types.ZarfComponent

	index int

	vars   []types.ZarfPackageVariable
	consts []types.ZarfPackageConstant

	relativeToHead string

	prev *Node
	next *Node
}

// GetIndex gives the component index of the node on it's original zarf file
func (n *Node) GetIndex() int {
	return n.index
}

// Path from downstream zarf file to upstream imported zarf file
func (n *Node) GetRelativeToHead() string {
	return n.relativeToHead
}

// Next node in the chain
func (n *Node) Next() *Node {
	return n.next
}

// Prev node in the chain
func (n *Node) Prev() *Node {
	return n.prev
}

// ImportName returns the name of the component to import
//
// If the component import has a ComponentName defined, that will be used
// otherwise the name of the component will be used
func (n *Node) ImportName() string {
	name := n.ZarfComponent.Name
	if n.Import.ComponentName != "" {
		name = n.Import.ComponentName
	}
	return name
}

// ImportChain is a doubly linked list of component import definitions
type ImportChain struct {
	Head *Node
	Tail *Node

	remote *oci.OrasRemote
}

func (ic *ImportChain) GetRemoteName() string {
	if ic.remote == nil {
		return ""
	}
	return ic.remote.Repo().Reference.String()
}

func (ic *ImportChain) append(c types.ZarfComponent, index int, relativeToHead string, vars []types.ZarfPackageVariable, consts []types.ZarfPackageConstant) {
	node := &Node{
		ZarfComponent:  c,
		index:          index,
		relativeToHead: relativeToHead,
		vars:           vars,
		consts:         consts,
		prev:           nil,
		next:           nil,
	}
	if ic.Head == nil {
		ic.Head = node
		ic.Tail = node
	} else {
		p := ic.Tail
		node.prev = p
		p.next = node
		ic.Tail = node
	}
}

// NewImportChain creates a new import chain from a component
func NewImportChain(head types.ZarfComponent, index int, arch, flavor string) (*ImportChain, error) {
	if arch == "" {
		return nil, fmt.Errorf("cannot build import chain: architecture must be provided")
	}

	ic := &ImportChain{}

	ic.append(head, index, ".", nil, nil)

	history := []string{}

	node := ic.Head
	for node != nil {
		isLocal := node.Import.Path != ""
		isRemote := node.Import.URL != ""

		if !isLocal && !isRemote {
			// This is the end of the import chain,
			// as the current node/component is not importing anything
			return ic, nil
		}

		// TODO: stuff like this should also happen in linting
		if err := validate.ImportDefinition(&node.ZarfComponent); err != nil {
			return ic, err
		}

		// ensure that remote components are not importing other remote components
		if node.prev != nil && node.prev.Import.URL != "" && isRemote {
			return ic, fmt.Errorf("detected malformed import chain, cannot import remote components from remote components")
		}
		// ensure that remote components are not importing local components
		if node.prev != nil && node.prev.Import.URL != "" && isLocal {
			return ic, fmt.Errorf("detected malformed import chain, cannot import local components from remote components")
		}

		var pkg types.ZarfPackage

		if isLocal {
			history = append(history, node.Import.Path)
			relativeToHead := filepath.Join(history...)

			// prevent circular imports (including self-imports)
			// this is O(n^2) but the import chain should be small
			prev := node
			for prev != nil {
				if prev.relativeToHead == relativeToHead {
					return ic, fmt.Errorf("detected circular import chain: %s", strings.Join(history, " -> "))
				}
				prev = prev.prev
			}

			// this assumes the composed package is following the zarf layout
			if err := utils.ReadYaml(filepath.Join(relativeToHead, layout.ZarfYAML), &pkg); err != nil {
				return ic, err
			}
		} else if isRemote {
			remote, err := ic.getRemote(node.Import.URL)
			if err != nil {
				return ic, err
			}
			pkg, err = remote.FetchZarfYAML()
			if err != nil {
				return ic, err
			}
		}

		name := node.ImportName()

		found := helpers.Filter(pkg.Components, func(c types.ZarfComponent) bool {
			matchesName := c.Name == name
			return matchesName && CompatibleComponent(c, arch, flavor)
		})

		if len(found) == 0 {
			if isLocal {
				return ic, fmt.Errorf("component %q not found in %q", name, filepath.Join(history...))
			} else if isRemote {
				return ic, fmt.Errorf("component %q not found in %q", name, node.Import.URL)
			}
		} else if len(found) > 1 {
			if isLocal {
				return ic, fmt.Errorf("multiple components named %q found in %q satisfying %q", name, filepath.Join(history...), arch)
			} else if isRemote {
				return ic, fmt.Errorf("multiple components named %q found in %q satisfying %q", name, node.Import.URL, arch)
			}
		}

		var index int
		//Probably can do this better, maybe have filter also give an index
		for i, component := range pkg.Components {
			if reflect.DeepEqual(found[0], component) {
				index = i
			}
		}
		ic.append(found[0], index, filepath.Join(history...), pkg.Variables, pkg.Constants)
		node = node.next
	}
	return ic, nil
}

// String returns a string representation of the import chain
func (ic *ImportChain) String() string {
	if ic.Head.next == nil {
		return fmt.Sprintf("component %q imports nothing", ic.Head.Name)
	}

	s := strings.Builder{}

	name := ic.Head.ImportName()

	if ic.Head.Import.Path != "" {
		s.WriteString(fmt.Sprintf("component %q imports %q in %s", ic.Head.Name, name, ic.Head.Import.Path))
	} else {
		s.WriteString(fmt.Sprintf("component %q imports %q in %s", ic.Head.Name, name, ic.Head.Import.URL))
	}

	node := ic.Head.next
	for node != ic.Tail {
		name := node.ImportName()
		s.WriteString(", which imports ")
		if node.Import.Path != "" {
			s.WriteString(fmt.Sprintf("%q in %s", name, node.Import.Path))
		} else {
			s.WriteString(fmt.Sprintf("%q in %s", name, node.Import.URL))
		}

		node = node.next
	}

	return s.String()
}

// Migrate performs migrations on the import chain
func (ic *ImportChain) Migrate(build types.ZarfBuildData) (warnings []string) {
	node := ic.Head
	for node != nil {
		migrated, w := deprecated.MigrateComponent(build, node.ZarfComponent)
		node.ZarfComponent = migrated
		warnings = append(warnings, w...)
		node = node.next
	}
	if len(warnings) > 0 {
		final := fmt.Sprintf("migrations were performed on the import chain of: %q", ic.Head.Name)
		warnings = append(warnings, final)
	}
	return warnings
}

// Compose merges the import chain into a single component
// fixing paths, overriding metadata, etc
func (ic *ImportChain) Compose() (composed types.ZarfComponent, err error) {
	composed = ic.Tail.ZarfComponent

	if ic.Tail.prev == nil {
		// only had one component in the import chain
		return composed, nil
	}

	if err := ic.fetchOCISkeleton(); err != nil {
		return composed, err
	}

	// start with an empty component to compose into
	composed = types.ZarfComponent{}

	// start overriding with the tail node
	node := ic.Tail
	for node != nil {
		fixPaths(&node.ZarfComponent, node.relativeToHead)

		// perform overrides here
		overrideMetadata(&composed, node.ZarfComponent)
		overrideDeprecated(&composed, node.ZarfComponent)
		overrideResources(&composed, node.ZarfComponent)
		overrideActions(&composed, node.ZarfComponent)

		composeExtensions(&composed, node.ZarfComponent, node.relativeToHead)

		node = node.prev
	}

	return composed, nil
}

// MergeVariables merges variables from the import chain
func (ic *ImportChain) MergeVariables(existing []types.ZarfPackageVariable) (merged []types.ZarfPackageVariable) {
	exists := func(v1 types.ZarfPackageVariable, v2 types.ZarfPackageVariable) bool {
		return v1.Name == v2.Name
	}

	node := ic.Tail
	for node != nil {
		// merge the vars
		merged = helpers.MergeSlices(node.vars, merged, exists)
		node = node.prev
	}
	merged = helpers.MergeSlices(existing, merged, exists)

	return merged
}

// MergeConstants merges constants from the import chain
func (ic *ImportChain) MergeConstants(existing []types.ZarfPackageConstant) (merged []types.ZarfPackageConstant) {
	exists := func(c1 types.ZarfPackageConstant, c2 types.ZarfPackageConstant) bool {
		return c1.Name == c2.Name
	}

	node := ic.Tail
	for node != nil {
		// merge the consts
		merged = helpers.MergeSlices(node.consts, merged, exists)
		node = node.prev
	}
	merged = helpers.MergeSlices(existing, merged, exists)

	return merged
}

// CompatibleComponent determines if this component is compatible with the given create options
func CompatibleComponent(c types.ZarfComponent, arch, flavor string) bool {
	satisfiesArch := c.Only.Cluster.Architecture == "" || c.Only.Cluster.Architecture == arch
	satisfiesFlavor := c.Only.Flavor == "" || c.Only.Flavor == flavor
	return satisfiesArch && satisfiesFlavor
}
