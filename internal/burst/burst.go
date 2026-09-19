package burst

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/prtvi/sorta/internal/exifmeta"
	"github.com/prtvi/sorta/internal/models"
)

// DefaultMaxGap is the maximum capture-time gap between consecutive burst frames.
const DefaultMaxGap = time.Second

// DefaultMinSize is the minimum cluster size to treat as a burst.
const DefaultMinSize = 3

// Item is a photo with optional capture time for clustering.
type Item struct {
	Name string
	Time time.Time // zero = unknown
}

// AnnotateDir reads capture times from files in dir, clusters bursts, and
// returns photos sorted by time with burst fields set. Original URLs are preserved.
func AnnotateDir(dir string, photos []models.Photo, maxGap time.Duration, minSize int) []models.Photo {
	if maxGap <= 0 {
		maxGap = DefaultMaxGap
	}
	if minSize < 2 {
		minSize = DefaultMinSize
	}

	byName := make(map[string]models.Photo, len(photos))
	items := make([]Item, len(photos))
	for i, p := range photos {
		byName[p.Name] = p
		items[i] = Item{Name: p.Name}
	}

	workers := runtime.NumCPU()
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t, ok := exifmeta.CaptureTime(filepath.Join(dir, items[i].Name)); ok {
				items[i].Time = t
			}
		}(i)
	}
	wg.Wait()

	sorted := ClusterAndOrder(items, maxGap, minSize)
	out := make([]models.Photo, 0, len(sorted))
	for _, a := range sorted {
		base := byName[a.Name]
		base.Name = a.Name
		base.BurstID = a.BurstID
		base.BurstIndex = a.BurstIndex
		base.BurstSize = a.BurstSize
		base.TakenAt = a.TakenAt
		if base.URL == "" {
			base.URL = "/photos/" + a.Name
		}
		out = append(out, base)
	}
	return out
}

// GroupBursts splits annotated photos into burst groups and singles (preserving order).
func GroupBursts(photos []models.Photo) (bursts []models.BurstGroup, singles []models.Photo) {
	bursts = []models.BurstGroup{}
	singles = []models.Photo{}
	seen := map[string]bool{}
	for _, p := range photos {
		if p.BurstID == "" || p.BurstSize < 2 {
			singles = append(singles, p)
			continue
		}
		if seen[p.BurstID] {
			continue
		}
		seen[p.BurstID] = true
		members := make([]models.Photo, 0, p.BurstSize)
		for _, q := range photos {
			if q.BurstID == p.BurstID {
				members = append(members, q)
			}
		}
		bursts = append(bursts, models.BurstGroup{
			ID:     p.BurstID,
			Size:   len(members),
			Photos: members,
		})
	}
	return bursts, singles
}

// Annotated is the result of clustering for one photo.
type Annotated struct {
	Name       string
	BurstID    string
	BurstIndex int
	BurstSize  int
	TakenAt    string
}

// ClusterAndOrder sorts by time then name, assigns burst annotations.
func ClusterAndOrder(items []Item, maxGap time.Duration, minSize int) []Annotated {
	type scored struct {
		Item
		orig int
	}
	scoredItems := make([]scored, len(items))
	for i, it := range items {
		scoredItems[i] = scored{Item: it, orig: i}
	}

	sort.SliceStable(scoredItems, func(i, j int) bool {
		a, b := scoredItems[i], scoredItems[j]
		aOK := !a.Time.IsZero()
		bOK := !b.Time.IsZero()
		switch {
		case aOK && bOK:
			if !a.Time.Equal(b.Time) {
				return a.Time.Before(b.Time)
			}
			return a.Name < b.Name
		case aOK && !bOK:
			return true
		case !aOK && bOK:
			return false
		default:
			return a.Name < b.Name
		}
	})

	n := len(scoredItems)
	groupOf := make([]int, n)
	for i := range groupOf {
		groupOf[i] = -1
	}

	gid := 0
	i := 0
	for i < n {
		if scoredItems[i].Time.IsZero() {
			i++
			continue
		}
		j := i + 1
		for j < n && !scoredItems[j].Time.IsZero() &&
			scoredItems[j].Time.Sub(scoredItems[j-1].Time) <= maxGap {
			j++
		}
		size := j - i
		if size >= minSize {
			for k := i; k < j; k++ {
				groupOf[k] = gid
			}
			gid++
		}
		i = j
	}

	sizes := map[int]int{}
	for _, g := range groupOf {
		if g >= 0 {
			sizes[g]++
		}
	}
	indexInGroup := map[int]int{}

	out := make([]Annotated, n)
	for i, s := range scoredItems {
		a := Annotated{Name: s.Name}
		if !s.Time.IsZero() {
			a.TakenAt = s.Time.Format("2006-01-02 15:04:05")
		}
		g := groupOf[i]
		if g >= 0 {
			idx := indexInGroup[g]
			indexInGroup[g] = idx + 1
			a.BurstID = fmt.Sprintf("b%d", g)
			a.BurstIndex = idx
			a.BurstSize = sizes[g]
		}
		out[i] = a
	}
	return out
}

// Members returns names sharing burstID from an annotated list (preserving order).
func Members(photos []models.Photo, burstID string) []string {
	if burstID == "" {
		return nil
	}
	var names []string
	for _, p := range photos {
		if p.BurstID == burstID {
			names = append(names, p.Name)
		}
	}
	return names
}
