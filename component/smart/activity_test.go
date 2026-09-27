package smart

import (
	"fmt"
	"testing"
)

func TestActiveTargetIndexIsIncrementalAndDirty(t *testing.T) {
	store := &Store{}
	group, config := "g-activity", "c-activity"
	clearTargetActivity("group", config, group)

	store.TouchActiveTarget(group, config, "a.example", false, 10)
	store.TouchActiveTarget(group, config, "b.example", true, 30)
	store.TouchActiveTarget(group, config, "a.example", false, 40)

	active := store.GetActiveTargets(group, config, 8)
	if len(active) != 2 || active[0].Target != "a.example" || active[0].LastUsed != 40 {
		t.Fatalf("active=%+v", active)
	}

	dirty := store.takeDirtyTargets(group, config, 1)
	if len(dirty) != 1 || dirty[0].Target != "a.example" {
		t.Fatalf("dirty first=%+v", dirty)
	}

	remaining := store.takeDirtyTargets(group, config, 8)
	if len(remaining) != 1 || remaining[0].Target != "b.example" {
		t.Fatalf("dirty remaining=%+v", remaining)
	}

	if again := store.takeDirtyTargets(group, config, 8); len(again) != 0 {
		t.Fatalf("dirty should be drained: %+v", again)
	}
}

func TestActiveTargetIndexClearsByGroup(t *testing.T) {
	store := &Store{}
	group, config := "g-clear", "c-clear"
	store.TouchActiveTarget(group, config, "x.example", false, 1)
	clearTargetActivity("group", config, group)
	if got := store.GetActiveTargets(group, config, 8); len(got) != 0 {
		t.Fatalf("active after clear=%+v", got)
	}
}


func TestActiveTargetIndexStaysBoundedAfterSaturation(t *testing.T) {
	store := &Store{}
	group, config := "g-cap", "c-cap"
	clearTargetActivity("group", config, group)

	for i := 0; i < activeTargetHardLimit+512; i++ {
		store.TouchActiveTarget(group, config, fmt.Sprintf("host-%d.example", i), false, int64(i+1))
	}

	active := store.GetActiveTargets(group, config, activeTargetHardLimit+1024)
	if len(active) != activeTargetHardLimit {
		t.Fatalf("active len=%d want=%d", len(active), activeTargetHardLimit)
	}
	if active[0].Target != fmt.Sprintf("host-%d.example", activeTargetHardLimit+511) {
		t.Fatalf("newest target missing: %+v", active[0])
	}
}
