package burst_test

import (
	"testing"
	"time"

	"github.com/prtvi/sorta/internal/burst"
	"github.com/prtvi/sorta/internal/models"
)

func TestClusterAndOrderDetectsBurst(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	items := []burst.Item{
		{Name: "c.jpg", Time: base.Add(400 * time.Millisecond)},
		{Name: "a.jpg", Time: base},
		{Name: "b.jpg", Time: base.Add(200 * time.Millisecond)},
		{Name: "solo.jpg", Time: base.Add(10 * time.Second)},
		{Name: "notime.jpg"},
	}
	out := burst.ClusterAndOrder(items, time.Second, 3)
	if len(out) != 5 {
		t.Fatalf("len=%d", len(out))
	}
	// Time order: a, b, c, solo, notime
	if out[0].Name != "a.jpg" || out[1].Name != "b.jpg" || out[2].Name != "c.jpg" {
		t.Fatalf("order %+v", out)
	}
	if out[0].BurstID == "" || out[0].BurstSize != 3 {
		t.Fatalf("a not in burst: %+v", out[0])
	}
	if out[0].BurstID != out[1].BurstID || out[1].BurstID != out[2].BurstID {
		t.Fatalf("burst ids mismatch")
	}
	if out[0].BurstIndex != 0 || out[1].BurstIndex != 1 || out[2].BurstIndex != 2 {
		t.Fatalf("indexes %+v", out[:3])
	}
	if out[3].BurstID != "" || out[4].BurstID != "" {
		t.Fatalf("solo/notime should not burst")
	}
}

func TestClusterIgnoresSmallGroups(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	items := []burst.Item{
		{Name: "a.jpg", Time: base},
		{Name: "b.jpg", Time: base.Add(100 * time.Millisecond)},
	}
	out := burst.ClusterAndOrder(items, time.Second, 3)
	if out[0].BurstID != "" || out[1].BurstID != "" {
		t.Fatalf("pair should not be a burst with minSize 3")
	}
}

func TestClusterSplitsOnGap(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	items := []burst.Item{
		{Name: "a.jpg", Time: base},
		{Name: "b.jpg", Time: base.Add(100 * time.Millisecond)},
		{Name: "c.jpg", Time: base.Add(200 * time.Millisecond)},
		{Name: "d.jpg", Time: base.Add(5 * time.Second)},
		{Name: "e.jpg", Time: base.Add(5*time.Second + 100*time.Millisecond)},
		{Name: "f.jpg", Time: base.Add(5*time.Second + 200*time.Millisecond)},
	}
	out := burst.ClusterAndOrder(items, time.Second, 3)
	if out[0].BurstID == out[3].BurstID {
		t.Fatal("two bursts should have different ids")
	}
	if out[0].BurstSize != 3 || out[3].BurstSize != 3 {
		t.Fatalf("sizes %+v", out)
	}
}

func TestGroupBursts(t *testing.T) {
	photos := []models.Photo{
		{Name: "a.jpg", BurstID: "b1", BurstSize: 3, BurstIndex: 0},
		{Name: "b.jpg", BurstID: "b1", BurstSize: 3, BurstIndex: 1},
		{Name: "c.jpg", BurstID: "b1", BurstSize: 3, BurstIndex: 2},
		{Name: "solo.jpg"},
	}
	bursts, singles := burst.GroupBursts(photos)
	if len(bursts) != 1 || bursts[0].Size != 3 || len(bursts[0].Photos) != 3 {
		t.Fatalf("bursts %+v", bursts)
	}
	if len(singles) != 1 || singles[0].Name != "solo.jpg" {
		t.Fatalf("singles %+v", singles)
	}
}
