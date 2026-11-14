package statistic

import (
	"encoding/json"
	"testing"
)

func TestP1TrafficSummaryAvoidsLiveConnectionSnapshot(t *testing.T) {
	m := &Manager{}
	m.PushUploaded(123)
	m.PushDownloaded(456)
	summary := m.Summary()
	if summary.UploadTotal != 123 || summary.DownloadTotal != 456 || summary.ConnectionCount != 0 {
		t.Fatalf("summary values differ: %+v", summary)
	}
	encoded, err := json.Marshal(summary)
	if err != nil { t.Fatal(err) }
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil { t.Fatal(err) }
	if _, ok := fields["connections"]; ok {
		t.Fatal("cheap dashboard summary exposed a full connection snapshot")
	}
	for _, key := range []string{"downloadTotal", "uploadTotal", "connectionCount", "memory"} {
		if _, ok := fields[key]; !ok { t.Fatalf("missing summary key %q", key) }
	}
}
