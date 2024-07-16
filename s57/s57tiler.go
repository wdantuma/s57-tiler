package s57

// Convert S-57 format to MVT vector tiles
// see MVT spec at https://github.com/mapbox/vector-tile-spec/tree/master/2.1

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lukeroth/gdal"
	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"github.com/wdantuma/s57-tiler/s57/vectortile"
	"github.com/wdantuma/signalk-server-go/ref"
	"github.com/wdantuma/signalk-server-go/resources/charts"
	"google.golang.org/protobuf/proto"
)

const (
	TILE_EXTENT = 4096
)

type ValueType int

const (
	VT_STRING ValueType = iota
	VT_INT
	VT_FLOAT
)

type Value struct {
	fieldType ValueType
	value     interface{}
}

type s57Tiler struct {
	minZoom   int
	maxZoom   int
	transform gdal.CoordinateTransform
	datasets  []dataset.Dataset
	valuesMap map[string]uint32
	values    []Value
	keysMap   map[string]uint32
	keys      []string
	lastx     int32
	lasty     int32
}

func NewS57Tiler(datasets []dataset.Dataset, minzoom int, maxzoom int) *s57Tiler {
	src := gdal.CreateSpatialReference("")
	src.FromEPSG(4326)
	dst := gdal.CreateSpatialReference("")
	dst.FromEPSG(3857)

	return &s57Tiler{transform: gdal.CreateCoordinateTransform(src, dst), datasets: datasets, minZoom: minzoom, maxZoom: maxzoom}
}

func (s *s57Tiler) startLayer() {
	s.valuesMap = make(map[string]uint32)
	s.values = make([]Value, 0)
	s.keysMap = make(map[string]uint32)
	s.keys = make([]string, 0)
}

func (s *s57Tiler) to3857(x float64, y float64) (float64, float64) {
	xs := make([]float64, 1)
	xs[0] = y
	ys := make([]float64, 1)
	ys[0] = x
	zs := make([]float64, 1)
	zs[0] = 0

	s.transform.Transform(1, xs, ys, zs)

	return xs[0], ys[0]
}

func (s *s57Tiler) toTileCoordinate(tileBounds m.Extrema, x float64, y float64, z float64) (int32, int32, int32) {
	tx, ty := s.to3857(x, y)

	ulx, uly := s.to3857(tileBounds.W, tileBounds.N)
	lrx, lry := s.to3857(tileBounds.E, tileBounds.S)

	xf := TILE_EXTENT / (lrx - ulx)
	yf := TILE_EXTENT / (uly - lry)
	xx := (tx - ulx) * xf
	yy := (uly - ty) * yf
	return int32(xx), int32(yy), 0
}

func getCommand(command int, count int) uint32 {
	cmd := (command & 0x7) | (count << 3)
	return uint32(cmd)
}

func getCoordinate(coordinate int32) uint32 {
	return uint32((coordinate << 1) ^ (coordinate >> 31))
}

func IsClockWise(geom *gdal.Geometry) bool {
	pc := geom.PointCount()
	if pc < 2 {
		return false
	}
	var sum float64 = 0
	for i := 0; i < pc-1; i++ {
		x1, y1, _ := geom.Point(i)
		x2, y2, _ := geom.Point(i + 1)
		sum += (x2 - x1) * (y2 + y1)
	}
	return sum > 0
}

func (s *s57Tiler) toMvtLinestringGeometry(geometry *gdal.Geometry, tileBounds m.Extrema, ccw bool) []uint32 {
	mvtGeometry := make([]uint32, 0)
	count := geometry.PointCount()
	index := 0
	step := 1
	clockWise := IsClockWise(geometry)
	if ccw && clockWise || !ccw && !clockWise {
		index = count - 1
		step = -1
	}
	if count > 1 {
		// moveto
		mvtGeometry = append(mvtGeometry, getCommand(1, 1))
		x, y, _ := geometry.Point(index)
		xx, yy, _ := s.toTileCoordinate(tileBounds, x, y, 0)
		dx := xx - s.lastx
		dy := yy - s.lasty
		s.lastx = xx
		s.lasty = yy
		mvtGeometry = append(mvtGeometry, getCoordinate(dx))
		mvtGeometry = append(mvtGeometry, getCoordinate(dy))
		// lineto
		mvtGeometry = append(mvtGeometry, getCommand(2, count-1))
		for i := 1; i < count; i++ {
			index += step
			x, y, _ := geometry.Point(index)
			xx, yy, _ := s.toTileCoordinate(tileBounds, x, y, 0)
			dx := xx - s.lastx
			dy := yy - s.lasty
			mvtGeometry = append(mvtGeometry, getCoordinate(dx))
			mvtGeometry = append(mvtGeometry, getCoordinate(dy))
			s.lastx = xx
			s.lasty = yy
		}
	}

	return mvtGeometry
}

func (s *s57Tiler) toMvtPolygonGeometry(geometry *gdal.Geometry, tileBounds m.Extrema, ccw bool) []uint32 {
	mvtGeometry := s.toMvtLinestringGeometry(geometry, tileBounds, ccw)
	// close path
	mvtGeometry = append(mvtGeometry, getCommand(7, 1))
	return mvtGeometry
}

func (s *s57Tiler) toMvtPointGeometry(geometry *gdal.Geometry, tileBounds m.Extrema) []uint32 {
	mvtGeometry := make([]uint32, 0)
	count := geometry.PointCount()
	mvtGeometry = append(mvtGeometry, getCommand(1, count))
	for i := 0; i < count; i++ {
		x, y, _ := geometry.Point(i)
		xx, yy, _ := s.toTileCoordinate(tileBounds, x, y, 0)
		dx := xx - s.lastx
		dy := yy - s.lasty
		mvtGeometry = append(mvtGeometry, getCoordinate(dx))
		mvtGeometry = append(mvtGeometry, getCoordinate(dy))
		s.lastx = xx
		s.lasty = yy
	}

	return mvtGeometry
}

func (s *s57Tiler) toMvtGeometry(featureType vectortile.Tile_GeomType, geometry *gdal.Geometry, tileBounds m.Extrema) []uint32 {
	s.lastx = 0
	s.lasty = 0
	mvtGeometry := make([]uint32, 0)
	geomcount := geometry.GeometryCount()
	pointCount := geometry.PointCount()
	if geomcount > 0 {
		for i := 0; i < geomcount; i++ {
			geom := geometry.Geometry(i)
			switch featureType {
			case vectortile.Tile_POINT:
				mvtGeometry = append(mvtGeometry, s.toMvtPointGeometry(&geom, tileBounds)...)
			case vectortile.Tile_LINESTRING:
				mvtGeometry = append(mvtGeometry, s.toMvtLinestringGeometry(&geom, tileBounds, false)...)
			case vectortile.Tile_POLYGON:
				mvtGeometry = append(mvtGeometry, s.toMvtPolygonGeometry(&geom, tileBounds, i > 0)...)
			}
		}
	} else if pointCount > 0 {
		switch featureType {
		case vectortile.Tile_POINT:
			mvtGeometry = append(mvtGeometry, s.toMvtPointGeometry(geometry, tileBounds)...)
		case vectortile.Tile_LINESTRING:
			mvtGeometry = append(mvtGeometry, s.toMvtLinestringGeometry(geometry, tileBounds, false)...)
		case vectortile.Tile_POLYGON:
			mvtGeometry = append(mvtGeometry, s.toMvtPolygonGeometry(geometry, tileBounds, false)...)
		}
	}

	return mvtGeometry
}

func (s *s57Tiler) getMvtFeatureType(geometry *gdal.Geometry) *vectortile.Tile_GeomType {
	geomType := geometry.Type()
	var mvtGeomType vectortile.Tile_GeomType
	switch geomType {
	case gdal.GT_LineString: //, gdal.GT_MultiLineString25D, gdal.GT_LineString25D, gdal.GT_MultiLineString:
		mvtGeomType = vectortile.Tile_LINESTRING
	case gdal.GT_Polygon: //, gdal.GT_MultiPolygon25D, gdal.GT_MultiPolygon, gdal.GT_Polygon25D:
		mvtGeomType = vectortile.Tile_POLYGON
	case gdal.GT_Point, gdal.GT_MultiPoint25D: //, gdal.GT_Point25D, gdal.GT_MultiPoint, gdal.GT_MultiPoint25D:
		mvtGeomType = vectortile.Tile_POINT
	default:
		mvtGeomType = vectortile.Tile_UNKNOWN
	}
	return &mvtGeomType
}

func (s *s57Tiler) toMvtFeature(feature *gdal.Feature, tileBounds m.Extrema) *vectortile.Tile_Feature {
	geom := feature.Geometry()
	mvtFeature := vectortile.Tile_Feature{}
	mvtFeature.Type = s.getMvtFeatureType(&geom)
	if *mvtFeature.Type != vectortile.Tile_UNKNOWN {
		// write tags
		for i := 0; i < feature.FieldCount(); i++ {
			fieldDef := feature.FieldDefinition(i)
			key := fieldDef.Name()
			var value interface{}
			fieldType := fieldDef.Type()
			vt := VT_STRING
			if feature.IsFieldSet(i) {
				switch fieldType {
				case gdal.FT_StringList:
					st := string(feature.FieldAsString(i))
					value = st[strings.Index(st, ":")+1 : len(st)-1]
					break
				case gdal.FT_Integer:
					vt = VT_INT
					value = feature.FieldAsInteger64(i)
					break
				case gdal.FT_Real:
					vt = VT_FLOAT
					value = feature.FieldAsFloat64(i)
					break
				default:
					value = feature.FieldAsString(i)
					break
				}
				if value != "" {
					if _, ok := s.keysMap[key]; !ok {
						s.keysMap[key] = uint32(len(s.keys))
						s.keys = append(s.keys, key)
					}
					vmk := ""
					switch vt {
					case VT_STRING:
						vmk = fmt.Sprintf("%d_%s", vt, value)
						break
					case VT_INT:
						vmk = fmt.Sprintf("%d_%d", vt, value)
						break
					case VT_FLOAT:
						vmk = fmt.Sprintf("%d_%f", vt, value)
						break
					}

					if _, ok := s.valuesMap[vmk]; !ok {
						s.valuesMap[vmk] = uint32(len(s.values))
						s.values = append(s.values, Value{fieldType: vt, value: value})
					}
					mvtFeature.Tags = append(mvtFeature.Tags, s.keysMap[key])
					mvtFeature.Tags = append(mvtFeature.Tags, s.valuesMap[vmk])
				}
			}
		}
		mvtFeature.Geometry = s.toMvtGeometry(*mvtFeature.Type, &geom, tileBounds)
		return &mvtFeature
	}
	return nil
}

func (s *s57Tiler) GetFeatures(layer gdal.Layer, tile m.TileID, tileBounds m.Extrema) []*vectortile.Tile_Feature {

	features := make([]*vectortile.Tile_Feature, 0)
	b2 := m.Bounds(m.TileID{X: tile.X + 1, Y: tile.Y, Z: tile.Z})
	buffer := math.Abs(tileBounds.E-b2.E) / 4
	bounds := m.Extrema{N: tileBounds.N + buffer, S: tileBounds.S - buffer, W: tileBounds.W - buffer, E: tileBounds.E + buffer}

	layer.SetSpatialFilterRect(bounds.W, bounds.S, bounds.E, bounds.N)

	ok := true

	for ok {
		feature := layer.NextFeature()
		if feature != nil {
			mvtFeature := s.toMvtFeature(feature, tileBounds)
			if mvtFeature != nil {
				features = append(features, mvtFeature)
			}
			feature.Destroy()
		} else {
			ok = false
		}
	}

	return features
}

func (s *s57Tiler) GetTilesForBounds(tiles map[string]m.TileID, bounds m.Extrema, zoomLevel int) map[string]m.TileID {
	if tiles == nil {
		tiles = make(map[string]m.TileID)
	}
	ulTile := m.Tile(bounds.W, bounds.N, zoomLevel)
	lrTile := m.Tile(bounds.E, bounds.S, zoomLevel)
	for col := ulTile.X; col <= lrTile.X; col++ {
		for row := ulTile.Y; row <= lrTile.Y; row++ {
			key := fmt.Sprintf("%d,%d,%d", col, row, zoomLevel)
			tile := m.TileID{X: col, Y: row, Z: uint64(zoomLevel)}
			tiles[key] = tile
		}
	}
	return tiles
}

func (s *s57Tiler) GetTiles(file dataset.File, zoomLevel int) map[string]m.TileID {
	tiles := make(map[string]m.TileID)
	datasource := gdal.OpenDataSource(file.Path, 0)
	defer datasource.Destroy()
	for i := 0; i < datasource.LayerCount(); i++ {
		l := datasource.LayerByIndex(i)
		ext, err := l.Extent(true)
		if err == nil {
			tiles = s.GetTilesForBounds(tiles, m.Extrema{W: ext.MinX(), N: ext.MaxY(), E: ext.MaxX(), S: ext.MinY()}, zoomLevel)
		}
	}
	return tiles
}

func getBounds(file dataset.File) []float32 {

	var bounds []float32
	layer, ok := file.Layers["M_COVR"]
	if ok {
		datasource := gdal.OpenDataSource(file.Path, 0)
		defer datasource.Destroy()
		bounds = make([]float32, 4)
		bounds[0] = float32(layer.Bounds.MinX())
		bounds[1] = float32(layer.Bounds.MinY())
		bounds[2] = float32(layer.Bounds.MaxX())
		bounds[3] = float32(layer.Bounds.MaxY())
	}

	return bounds
}

func (s *s57Tiler) GenerateMetaData(outPath string, dataset dataset.Dataset, file dataset.File) {
	path := filepath.Join(outPath, file.Id, "metadata.json")
	bounds := getBounds(file)
	metaData := charts.ChartMetaData{Id: file.Id, Name: file.Id, Description: dataset.Description, Created: time.Now().UTC(), Type: "S-57", Format: "pbf", MinZoom: s.minZoom, MaxZoom: s.maxZoom, Bounds: bounds}

	out, _ := json.Marshal(metaData)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(path), 0700) // Create your file
	}
	err := os.WriteFile(path, out, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

func (s *s57Tiler) GenerateTile(outPath string, file dataset.File, tile m.TileID) {
	mvtTile := vectortile.Tile{}

	//allowedLayers := []string{"BOYLAT", "BOYCAR", "BOYINB", "BOYISD", "BOYSAW", "BOYSPP", "BCNLAT", "BCNCAR", "BCNISN", "BCNSAW", "BCNSPP", "LIGHTS", "DEPARE", "SEAARE", "COALNE", "RESARE", "UNSARE", "LNDARE", "BUAARE", "NAVLNE", "RECTRC", "CANALS"}

	bounds := m.Bounds(tile)
	tileEnvelope := gdal.Envelope{}
	tileEnvelope.SetMaxX(bounds.E)
	tileEnvelope.SetMaxY(bounds.N)
	tileEnvelope.SetMinX(bounds.W)
	tileEnvelope.SetMinY(bounds.S)

	for layerName, layer := range file.Layers {
		ln := layerName
		var version uint32 = 2
		var extent uint32 = TILE_EXTENT
		s.startLayer()
		mvtLayer := vectortile.Tile_Layer{Name: &ln, Version: &version, Extent: &extent}
		datasource := gdal.OpenDataSource(file.Path, 0)
		defer datasource.Destroy()
		if layer.Bounds.Intersects(tileEnvelope) {
			l := datasource.LayerByName(layerName)
			c, ok := l.FeatureCount(false)
			if ok && c > 0 {
				features := s.GetFeatures(l, tile, bounds)
				mvtLayer.Features = append(mvtLayer.Features, features...)
			}
		}
		if len(mvtLayer.Features) > 0 {
			// keys
			for _, k := range s.keys {
				mvtLayer.Keys = append(mvtLayer.Keys, k)
			}
			// values
			for _, v := range s.values {
				value := vectortile.Tile_Value{}
				switch v.fieldType {
				case VT_STRING:
					value.StringValue = ref.String(v.value)
					break
				case VT_FLOAT:
					value.DoubleValue = ref.Float64(v.value)
					break
				case VT_INT:
					value.IntValue = ref.Int64((v.value))
					break
				}

				mvtLayer.Values = append(mvtLayer.Values, &value)
			}

			mvtTile.Layers = append(mvtTile.Layers, &mvtLayer)
		}
	}

	path := filepath.Join(outPath, file.Id, strconv.Itoa(int(tile.Z)), strconv.Itoa(int(tile.X)), strconv.Itoa(int(tile.Y))) + ".pbf"
	if len(mvtTile.Layers) > 0 {
		out, _ := proto.Marshal(&mvtTile)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(filepath.Dir(path), 0700) // Create your file
		}
		err := os.WriteFile(path, out, 0644)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		os.Remove(path)
	}
}
