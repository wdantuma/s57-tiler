package dataset

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lukeroth/gdal"
	"github.com/tburke/iso8211"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// ConfigureGDAL sets the process-global GDAL S-57 reader options. It must be called
// once before any datasource is opened — the CLI calls it at startup, and test
// packages call it from TestMain. It replaces an init() so that merely importing
// this package no longer mutates the process environment as a side effect (which
// was a test-ordering / library-purity hazard).
//
//   - SPLIT_MULTIPOINT/ADD_SOUNDG_DEPTH: emit each depth sounding as its own 3D point
//     feature with a real DEPTH attribute (otherwise SOUNDG is a multipoint whose depth
//     lives only in the geometry Z and is lost during tiling).
func ConfigureGDAL() {
	os.Setenv("OGR_S57_OPTIONS", "SPLIT_MULTIPOINT=ON,ADD_SOUNDG_DEPTH=ON")
	os.Setenv("OGR_GEOMETRY_ACCEPT_UNCLOSED_RING", "NO")
}

type Layer struct {
	Name   string
	Bounds gdal.Envelope
}

type File struct {
	Id     string
	Path   string
	Title  string
	Layers map[string]Layer
}

type Dataset struct {
	Id          string
	Description string
	Files       []File
}

func getLayers(datasource gdal.DataSource) map[string]Layer {
	layers := make(map[string]Layer, 0)
	for i := 0; i < datasource.LayerCount(); i++ {
		layer := datasource.LayerByIndex(i)
		extent, err := layer.Extent(false)
		if err == nil {
			layers[layer.Name()] = Layer{Name: layer.Name(), Bounds: extent}
		}
	}
	return layers
}

func (dataset Dataset) GetLayers() []string {
	layersMap := make(map[string]int)
	for _, f := range dataset.Files {
		for _, l := range f.Layers {
			layersMap[l.Name] = 1
		}
	}
	layers := make([]string, 0)
	for k := range layersMap {
		layers = append(layers, k)
	}
	return layers
}

func (file File) LayerExists(layerName string) bool {
	_, ok := file.Layers[layerName]
	return ok
}

func (dataset Dataset) GetDatasetForTile(tile m.TileID) Dataset {
	retVal := Dataset{}
	for _, f := range dataset.Files {

		bounds := m.Bounds(tile)
		tileEnvelope := gdal.Envelope{}
		tileEnvelope.SetMaxX(bounds.E)
		tileEnvelope.SetMaxY(bounds.N)
		tileEnvelope.SetMinX(bounds.W)
		tileEnvelope.SetMinY(bounds.S)
		for _, layer := range f.Layers {
			if layer.Bounds.Intersects(tileEnvelope) {
				retVal.Files = append(retVal.Files, f)
				break
			}
		}
	}
	return retVal
}

// catalogCellRef extracts a cell's .000 file name and human title from a catalog
// DataRecord, bounds-checking every field/subfield access so a truncated or
// unexpected record is skipped (ok=false) rather than panicking. The subfield
// layout mirrors the S-57 catalog: index 2 = file name, 3 = title, 5 = record
// type ("BIN" for the binary cell files we tile).
func catalogCellRef(rec iso8211.DataRecord) (fileName, title string, ok bool) {
	if len(rec.Fields) < 2 {
		return "", "", false
	}
	sf := rec.Fields[1].SubFields
	if len(sf) < 6 || sf[5] != "BIN" {
		return "", "", false
	}
	fileName = fmt.Sprintf("%s", sf[2])
	if !strings.Contains(fileName, ".000") {
		return "", "", false
	}
	if len(sf) > 3 {
		title = strings.TrimSpace(fmt.Sprintf("%s", sf[3]))
	}
	return fileName, title, true
}

// safeCellID derives the chart id from a cell file path (its base name minus the
// ".000" suffix) and rejects anything that isn't a single, safe path element, so
// a crafted catalog can't make the id (".."/"."/a separator/empty) escape the
// output directory when it is later joined into the tile path.
func safeCellID(filePath string) (string, bool) {
	base := filepath.Base(filePath)
	if len(base) < 4 {
		return "", false
	}
	id := base[:len(base)-4]
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", false
	}
	return id, true
}

func GetS57Datasets(path string) ([]Dataset, error) {
	datasets := make([]Dataset, 0)
	err := filepath.WalkDir(path, func(fp string, entry fs.DirEntry, err error) error {
		if entry != nil {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if strings.ToUpper(info.Name()) == "CATALOG.031" {
				parts := strings.Split(fp, string(os.PathSeparator))
				id := "test"
				if len(parts) > 2 {
					id = parts[len(parts)-3]
				}
				dataset := Dataset{Id: id, Description: ""}
				f, err := os.Open(fp)
				if err != nil {
					return err
				}
				defer f.Close()
				var l iso8211.LeadRecord
				l.Read(f)
				var d iso8211.DataRecord
				d.Lead = &l
				for d.Read(f) == nil {
					// The catalog's LFIL ("long file name") subfield carries the cell's
					// human-readable title (e.g. "Anacortes and Vicinity, WA"), used as the
					// chart Description; absent in some older downloads.
					fileName, title, ok := catalogCellRef(d)
					if !ok {
						continue
					}
					filePath := strings.ReplaceAll(fileName, "\\", string(os.PathSeparator))
					filePath = filepath.Join(filepath.Dir(fp), filePath)
					cellID, ok := safeCellID(filePath)
					if !ok {
						fmt.Fprintf(os.Stderr, "skipping cell with unsafe name %q in %s\n", fileName, fp)
						continue
					}
					// Skip cells the catalog references but that aren't present/readable,
					// so a dangling entry never reaches OpenDataSource (whose binding can't
					// report a failed open).
					if _, statErr := os.Stat(filePath); statErr != nil {
						fmt.Fprintf(os.Stderr, "skipping missing cell %s: %v\n", filePath, statErr)
						continue
					}
					// Open, enumerate layers, and release the handle immediately rather
					// than deferring (a deferred Destroy here would keep every cell in the
					// catalog open until the whole walk returns, exhausting file descriptors
					// on large catalogs).
					datasource := gdal.OpenDataSource(filePath, 0)
					layers := getLayers(datasource)
					datasource.Destroy()
					dataset.Files = append(dataset.Files, File{
						Id:     cellID,
						Path:   filePath,
						Title:  title,
						Layers: layers,
					})
				}
				datasets = append(datasets, dataset)

			}
		} else {
			return errors.New(fmt.Sprintf("Invalid path:%s", path))
		}

		return nil
	})
	if err != nil {
		return datasets, err
	}
	return datasets, nil
}
