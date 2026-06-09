package s57

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"github.com/wdantuma/s57-tiler/s57/vectortile"
)

// TestDenseTileFingerprint prints a canonical, layer-order-independent fingerprint of
// the dense z14 tile of US4WA1JJ. It is a manual guard for performance refactors that
// must not change tile output: run it with -v before and after a change and confirm the
// fingerprint is identical.
//
//	go test ./s57/ -run TestDenseTileFingerprint -v
//
// (The value depends on the local GDAL version, so it isn't asserted against a constant.)
func TestDenseTileFingerprint(t *testing.T) {
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		t.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}
	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}
	var file *dataset.File
	for di := range datasets {
		for fi := range datasets[di].Files {
			if datasets[di].Files[fi].Id == "US4WA1JJ" {
				file = &datasets[di].Files[fi]
			}
		}
	}
	if file == nil {
		t.Skip("US4WA1JJ not found in sample ENC")
	}
	mcovr, ok := file.Layers["M_COVR"]
	if !ok {
		t.Skip("fixture has no M_COVR layer")
	}
	lon := (mcovr.Bounds.MinX() + mcovr.Bounds.MaxX()) / 2
	lat := (mcovr.Bounds.MinY() + mcovr.Bounds.MaxY()) / 2
	tile := m.Tile(lon, lat, 14)

	tiler := NewS57Tiler(datasets)
	tmp := t.TempDir()
	tiler.GenerateTile(tmp, *file, tile)
	pbf := filepath.Join(tmp, file.Id, strconv.Itoa(int(tile.Z)),
		strconv.Itoa(int(tile.X)), strconv.Itoa(int(tile.Y))+".pbf")
	raw, err := os.ReadFile(pbf)
	if err != nil {
		t.Fatalf("dense tile not written: %v", err)
	}
	var decoded vectortile.Tile
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode tile: %v", err)
	}

	t.Logf("dense tile z%d/%d/%d fingerprint: %s", tile.Z, tile.X, tile.Y, tileFingerprint(&decoded))
}

// tileFingerprint produces a stable hash of a tile's content that ignores layer
// ordering (Go map iteration over file.Layers is randomized) but is sensitive to every
// feature's geometry command stream and (key,value) tags.
func tileFingerprint(tile *vectortile.Tile) string {
	lines := make([]string, 0, len(tile.Layers))
	for _, l := range tile.Layers {
		h := fnv.New64a()
		for _, f := range l.Features {
			ft := int32(-1)
			if f.Type != nil {
				ft = int32(*f.Type)
			}
			fmt.Fprintf(h, "t%d|", ft)
			for _, g := range f.Geometry {
				fmt.Fprintf(h, "%d,", g)
			}
			kv := make([]string, 0, len(f.Tags)/2)
			for i := 0; i+1 < len(f.Tags); i += 2 {
				kv = append(kv, l.Keys[f.Tags[i]]+"="+valueString(l.Values[f.Tags[i+1]]))
			}
			sort.Strings(kv)
			for _, s := range kv {
				fmt.Fprintf(h, "%s;", s)
			}
			fmt.Fprint(h, "||")
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%x", l.GetName(), len(l.Features), h.Sum64()))
	}
	sort.Strings(lines)
	total := fnv.New64a()
	for _, ln := range lines {
		fmt.Fprintln(total, ln)
	}
	return fmt.Sprintf("%x (%d layers)", total.Sum64(), len(lines))
}

// valueString renders the value kinds the tiler emits (string, double, int).
func valueString(v *vectortile.Tile_Value) string {
	switch {
	case v == nil:
		return ""
	case v.StringValue != nil:
		return "s:" + *v.StringValue
	case v.DoubleValue != nil:
		return "d:" + strconv.FormatFloat(*v.DoubleValue, 'g', -1, 64)
	case v.IntValue != nil:
		return "i:" + strconv.FormatInt(*v.IntValue, 10)
	}
	return ""
}
