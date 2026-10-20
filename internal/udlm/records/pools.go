package records

import (
	"fmt"
	"sort"
	"strings"
)

// A cluster's node groups are not KubernetesCluster elements. In UDLM every
// node pool is a KubernetesNodePool record contained by its cluster (row 072),
// and the cluster carries no pools. The writer splits upstream's
// `nodes.{control_plane,workers}` out of a cluster spec into one pool per
// group. Each pool references its cluster through `cluster_ref`; the cluster's
// records never name the pools, so the writer finds them again by containment.

const (
	poolClass         = "KubernetesNodePool"
	clusterRefElement = "cluster_ref"
	nodesPath         = "nodes"
)

// nodeGroups maps an upstream node group to the pool name it becomes. The name
// is the only place the group survives: the pool class has no role element,
// and the writer does not invent labels or taints to stand in for one.
var nodeGroups = map[string]string{"control_plane": "control-plane", "workers": "workers"}

// nodeKeys maps an upstream node-group field to its pool element path.
var nodeKeys = map[string]string{
	"count":  "node_count",
	"cpu":    "node_resources.cpu.count",
	"memory": "node_resources.memory.size",
}

// nodeNoHome names node-group fields the pool class has no element for, with why.
var nodeNoHome = map[string]string{
	"storage": "per-node disk size has no KubernetesNodePool element yet, udlm#650",
}

// splitPools removes a cluster's node groups from fields and returns them as
// KubernetesNodePool fields, one per non-empty group, in pool-name order.
func (w *Writer) splitPools(serviceType string, fields map[string]any) ([]companion, error) {
	if serviceType != "cluster" {
		return nil, nil
	}
	raw, ok := fields[nodesPath]
	if !ok || isEmpty(raw) {
		return nil, nil
	}
	nodes, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an object", nodesPath)
	}
	var unknown []string
	for k := range nodes {
		if _, ok := nodeGroups[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("%s has groups with no pool mapping: %s", nodesPath, strings.Join(unknown, ", "))
	}
	t, err := w.class(poolClass, nodesPath)
	if err != nil {
		return nil, err
	}
	var pools []companion
	for group, name := range nodeGroups {
		g, ok := nodes[group]
		if !ok || isEmpty(g) {
			continue
		}
		pool, err := poolFields(name, g)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", nodesPath, group, err)
		}
		pools = append(pools, companion{t: t, name: name, fields: pool})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].name < pools[j].name })
	delete(fields, nodesPath)
	return pools, nil
}

// poolFields maps one upstream node group to pool fields. A field with no home
// on the pool is refused, not dropped (DSP-004).
func poolFields(name string, g any) (map[string]any, error) {
	group, ok := g.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not an object")
	}
	var homeless []string
	pool := map[string]any{"name": name}
	for k, v := range group {
		to, ok := nodeKeys[k]
		if !ok {
			why := nodeNoHome[k]
			if why == "" {
				why = "no KubernetesNodePool element"
			}
			homeless = append(homeless, k+" ("+why+")")
			continue
		}
		setPath(pool, to, v)
	}
	if len(homeless) > 0 {
		sort.Strings(homeless)
		return nil, fmt.Errorf("fields with no home on a %s: %s", poolClass, strings.Join(homeless, "; "))
	}
	return pool, nil
}
