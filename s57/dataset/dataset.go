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

type Layer struct {
	Name   string
	Bounds gdal.Envelope
}

type File struct {
	Id     string
	Path   string
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
				var l iso8211.LeadRecord
				l.Read(f)
				var d iso8211.DataRecord
				d.Lead = &l
				for d.Read(f) == nil {
					if d.Fields[1].SubFields[5] == "BIN" {
						fileName := fmt.Sprintf("%s", d.Fields[1].SubFields[2])
						if strings.Contains(fileName, ".000") {
							filePath := strings.ReplaceAll(fileName, "\\", string(os.PathSeparator))
							filePath = filepath.Join(filepath.Dir(fp), filePath)
							datasource := gdal.OpenDataSource(filePath, 0)
							parts = strings.Split(filePath, string(os.PathSeparator))
							file := File{
								Id:     parts[len(parts)-2],
								Path:   filePath,
								Layers: getLayers(datasource),
							}
							dataset.Files = append(dataset.Files, file)
							datasource.Release()
						}

					}

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
