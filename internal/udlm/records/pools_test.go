package records

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func clusterWithNodes(nodes map[string]any) map[string]any {
	return map[string]any{"service_type": "cluster", "version": "1.30", "nodes": nodes}
}

// A cluster's node groups become KubernetesNodePool entities contained by the
// cluster, each with its own intent, requested and realized records. The
// cluster's records carry no pools.
func TestWriterExpandsClusterNodesIntoNodePools(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	cluster := "ba7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := clusterWithNodes(map[string]any{
		"control_plane": map[string]any{"count": 3, "cpu": 4, "memory": "16GB"},
		"workers":       map[string]any{"count": 2, "cpu": 8, "memory": "32GB"},
	})
	w.Intent(ctx, IntentInput{EntityUUID: cluster, Spec: spec, Name: "dev"})
	w.Requested(ctx, RequestedInput{EntityUUID: cluster, Spec: spec, AgentName: "agent-k"})
	w.Realized(ctx, RealizedInput{EntityUUID: cluster, Outputs: map[string]any{"api_endpoint": "https://api.example"}, AgentName: "agent-k"})

	cRecs, _ := st.ListByEntity(ctx, cluster)
	if got := statesOf(cRecs); got != "Intent,Requested,Realized" {
		t.Fatalf("cluster states = %s", got)
	}
	for _, r := range cRecs {
		f := r.Body["fields"].(map[string]any)
		if _, ok := f["nodes"]; ok {
			t.Errorf("%s cluster record still carries nodes", r.State)
		}
		if f["release"] != "1.30" {
			t.Errorf("%s cluster record release = %v", r.State, f["release"])
		}
	}
	cReq := cRecs[1].Body
	if _, ok := cReq["dependencies"]; ok {
		t.Errorf("the cluster names no pools, so its record carries no pool edge: %v", cReq["dependencies"])
	}

	pools, err := st.Contained(ctx, cluster)
	if err != nil || len(pools) != 2 {
		t.Fatalf("want two pools contained by the cluster, got %v (%v)", pools, err)
	}
	byName := map[string][]StateRecord{}
	for _, p := range pools {
		recs, _ := st.ListByEntity(ctx, p)
		if got := statesOf(recs); got != "Intent,Requested,Realized" {
			t.Fatalf("pool %s states = %s", p, got)
		}
		for _, r := range recs {
			if r.ResourceType != "KubernetesNodePool" {
				t.Errorf("pool record resource_type = %s", r.ResourceType)
			}
			if err := Validate(r.Body); err != nil {
				t.Errorf("pool %s record invalid: %v", r.State, err)
			}
			if err := Verify(r.Body); err != nil {
				t.Errorf("pool %s record does not verify: %v", r.State, err)
			}
		}
		byName[stringOf(recs[0].Body["fields"].(map[string]any)["name"])] = recs
	}
	cp := byName["control-plane"]
	if cp == nil || byName["workers"] == nil {
		t.Fatalf("want pools named control-plane and workers, got %v", byName)
	}
	if got := fmt.Sprint(cp[0].Body["fields"]); got != "map[cluster_ref:uuid/"+cluster+" name:control-plane node_count:3 node_resources:map[cpu:map[count:4] memory:map[size:16GB]]]" {
		t.Errorf("control-plane intent fields = %s", got)
	}
	if cp[0].Body["metadata"].(map[string]any)["display_name"] != "dev control-plane" {
		t.Errorf("pool display name = %v", cp[0].Body["metadata"])
	}
	req := cp[1].Body
	// DEP-015: a pool is a child of the cluster's order.
	if req["root_request_uuid"] != cReq["record_uuid"] {
		t.Errorf("pool root_request_uuid = %v, want the cluster's requested record %v", req["root_request_uuid"], cReq["record_uuid"])
	}
	edge := fmt.Sprint(req["dependencies"])
	if !strings.Contains(edge, "contained_by") || !strings.Contains(edge, cluster) || !strings.Contains(edge, "relation:cluster") {
		t.Errorf("pool requested record should carry the contained_by edge to its cluster: %s", edge)
	}
	d := req["dispatch"].(map[string]any)
	if d["provider"] != "dcm/agents/agent-k" || fmt.Sprint(d["admitted"]) != "[name node_count node_resources.cpu.count node_resources.memory.size]" {
		t.Errorf("pool receipt = %v", d)
	}
	if out := cp[2].Body["outputs"].(map[string]any); len(out) != 0 {
		t.Errorf("the agent reports nothing per pool, so a pool's outputs must be empty: %v", out)
	}
}

// A node group's single disk size becomes the pool's node_disks: one boot disk.
func TestWriterMapsNodeStorageOntoNodeDisks(t *testing.T) {
	ctx := context.Background()
	w, st := testWriter(t)
	cluster := "da7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
	spec := clusterWithNodes(map[string]any{"workers": map[string]any{"count": 2, "storage": "120GB"}})
	w.Intent(ctx, IntentInput{EntityUUID: cluster, Spec: spec, Name: "dev"})
	pools, err := st.Contained(ctx, cluster)
	if err != nil || len(pools) != 1 {
		t.Fatalf("want one pool, got %v (%v)", pools, err)
	}
	recs, _ := st.ListByEntity(ctx, pools[0])
	got := fmt.Sprint(recs[0].Body["fields"].(map[string]any)["node_disks"])
	if got != "[map[name:root role:boot size:120GB]]" {
		t.Errorf("node_disks = %s", got)
	}
}

func TestWriterRefusesNodeGroupsItCannotPlace(t *testing.T) {
	ctx := context.Background()
	cases := map[string]map[string]any{
		"unknown group": {"infra": map[string]any{"count": 2}},
	}
	for name, nodes := range cases {
		t.Run(name, func(t *testing.T) {
			w, st := testWriter(t)
			cluster := "ca7c9a41-8e3d-4b6a-9c15-7d2e4f8a1b03"
			w.Intent(ctx, IntentInput{EntityUUID: cluster, Spec: clusterWithNodes(nodes)})
			if recs, _ := st.ListByEntity(ctx, cluster); len(recs) != 0 {
				t.Fatalf("want no record, got %d", len(recs))
			}
		})
	}
}
