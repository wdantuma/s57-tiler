package s57

// Convert S-57 format to MVT vector tiles
// see MVT spec at https://github.com/mapbox/vector-tile-spec/tree/master/2.1

import (
	"encoding/json"
	"fmt"
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
	TILE_EXTENT                   = 4096
	TILE_DIMENSION_AT_0   float64 = 360 //40075016.686
	SIMPLIFICATION_FACTOR         = 1
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
	srcRef    gdal.SpatialReference
	dstRef    gdal.SpatialReference
	transform gdal.CoordinateTransform
	closed    bool
	datasets  []dataset.Dataset
	valuesMap map[string]uint32
	values    []Value
	keysMap   map[string]uint32
	keys      []string
	lastx     int32
	lasty     int32

	// Reusable single-coordinate transform buffers, so to3857 doesn't allocate
	// per call. Per-tiler (one goroutine owns each tiler), so no sharing.
	bx, by, bz []float64

	// Cached tile projection: the 3857 origin and tile-unit scale, recomputed
	// only when the tile bounds change (they are constant for a whole tile).
	projBounds       m.Extrema
	projSet          bool
	ulx, uly, xf, yf float64

	// Cached open datasource, reused across every tile this tiler generates for a
	// file instead of reopening (and re-parsing) the S-57 cell each tile. Per-tiler
	// (one goroutine per tiler), so the non-thread-safe handle is never shared.
	// Must be released with Close().
	ds     gdal.DataSource
	dsPath string
	dsOpen bool
}

// datasource returns a cached open handle for path, opening it on first use and
// reopening only if the path changes. The caller must not Destroy the result; its
// lifetime is owned by the tiler and released by Close().
func (s *s57Tiler) datasource(path string) gdal.DataSource {
	if s.dsOpen && s.dsPath == path {
		return s.ds
	}
	if s.dsOpen {
		s.ds.Destroy()
	}
	s.ds = gdal.OpenDataSource(path, 0)
	s.dsPath = path
	s.dsOpen = true
	return s.ds
}

// Close releases the cached datasource and the coordinate-transform / spatial-
// reference handles created in NewS57Tiler. A worker must defer this when its
// tiler is done; the tiler must not be used afterwards. Safe to call more than once.
func (s *s57Tiler) Close() {
	if s.closed {
		return
	}
	s.closed = true
	if s.dsOpen {
		s.ds.Destroy()
		s.dsOpen = false
	}
	s.transform.Destroy()
	s.srcRef.Destroy()
	s.dstRef.Destroy()
}

func NewS57Tiler(datasets []dataset.Dataset) *s57Tiler {
	src := gdal.CreateSpatialReference("")
	src.FromEPSG(4326)
	dst := gdal.CreateSpatialReference("")
	dst.FromEPSG(3857)

	return &s57Tiler{
		srcRef:    src,
		dstRef:    dst,
		transform: gdal.CreateCoordinateTransform(src, dst),
		datasets:  datasets,
		bx:        make([]float64, 1),
		by:        make([]float64, 1),
		bz:        make([]float64, 1),
	}
}

func (s *s57Tiler) startLayer() {
	s.valuesMap = make(map[string]uint32)
	s.values = make([]Value, 0)
	s.keysMap = make(map[string]uint32)
	s.keys = make([]string, 0)
}

func (s *s57Tiler) to3857(x float64, y float64) (float64, float64) {
	// The transform expects geographic coordinates in (lat, lon) order, so y goes
	// into the first array and x into the second (preserved from the original).
	s.bx[0] = y
	s.by[0] = x
	s.bz[0] = 0

	s.transform.Transform(1, s.bx, s.by, s.bz)

	return s.bx[0], s.by[0]
}

// setProjection caches the tile's 3857 origin (ulx, uly) and tile-unit scale
// (xf, yf). The tile bounds are constant for a whole tile, so this recomputes the
// two corner transforms only when the bounds change instead of once per vertex.
func (s *s57Tiler) setProjection(tileBounds m.Extrema) {
	if s.projSet && tileBounds == s.projBounds {
		return
	}
	ulx, uly := s.to3857(tileBounds.W, tileBounds.N)
	lrx, lry := s.to3857(tileBounds.E, tileBounds.S)
	s.ulx = ulx
	s.uly = uly
	s.xf = TILE_EXTENT / (lrx - ulx)
	s.yf = TILE_EXTENT / (uly - lry)
	s.projBounds = tileBounds
	s.projSet = true
}

func (s *s57Tiler) toTileCoordinate(tileBounds m.Extrema, x float64, y float64, z float64) (int32, int32, int32) {
	s.setProjection(tileBounds)
	tx, ty := s.to3857(x, y)
	xx := (tx - s.ulx) * s.xf
	yy := (s.uly - ty) * s.yf
	return clampToInt32(xx), clampToInt32(yy), 0
}

// clampToInt32 converts a tile-space coordinate to int32 without silently
// wrapping: a pathological geometry or projection result (or NaN/±Inf) would
// otherwise overflow the cast and produce a bogus tile coordinate.
func clampToInt32(v float64) int32 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	default:
		return int32(v)
	}
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

	if count > 1 {
		index := 0
		step := 1
		clockWise := IsClockWise(geometry)
		if ccw && clockWise || !ccw && !clockWise {
			index = count - 1
			step = -1
		}
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

func (s *s57Tiler) toMvtGeometry(featureType vectortile.Tile_GeomType, geometry *gdal.Geometry, tile m.TileID, tileBounds m.Extrema) []uint32 {
	s.lastx = 0
	s.lasty = 0
	mvtGeometry := make([]uint32, 0)

	tolerance := TILE_DIMENSION_AT_0 / math.Pow(2, float64(tile.Z)) / 256 * SIMPLIFICATION_FACTOR

	simplifiedGeometry := geometry.SimplifyPreservingTopology(tolerance)
	defer simplifiedGeometry.Destroy()

	// Polygons need ring-aware handling: a Polygon's sub-geometries are its rings
	// (exterior + holes), but a MultiPolygon's sub-geometries are whole polygons.
	// emitPolygonRings descends to the rings in either case, so multipolygons are no
	// longer dropped and hole winding (ccw for rings after the first) stays correct.
	if featureType == vectortile.Tile_POLYGON {
		gt := simplifiedGeometry.Type()
		if gt == gdal.GT_MultiPolygon || gt == gdal.GT_MultiPolygon25D {
			for i := 0; i < simplifiedGeometry.GeometryCount(); i++ {
				poly := simplifiedGeometry.Geometry(i)
				mvtGeometry = append(mvtGeometry, s.emitPolygonRings(&poly, tileBounds)...)
			}
		} else {
			mvtGeometry = append(mvtGeometry, s.emitPolygonRings(&simplifiedGeometry, tileBounds)...)
		}
		return mvtGeometry
	}

	geomcount := simplifiedGeometry.GeometryCount()
	pointCount := simplifiedGeometry.PointCount()

	if geomcount > 0 {
		for i := 0; i < geomcount; i++ {
			geom := simplifiedGeometry.Geometry(i)
			switch featureType {
			case vectortile.Tile_POINT:
				mvtGeometry = append(mvtGeometry, s.toMvtPointGeometry(&geom, tileBounds)...)
			case vectortile.Tile_LINESTRING:
				mvtGeometry = append(mvtGeometry, s.toMvtLinestringGeometry(&geom, tileBounds, false)...)
			}
		}
	} else if pointCount > 0 {
		switch featureType {
		case vectortile.Tile_POINT:
			mvtGeometry = append(mvtGeometry, s.toMvtPointGeometry(&simplifiedGeometry, tileBounds)...)
		case vectortile.Tile_LINESTRING:
			mvtGeometry = append(mvtGeometry, s.toMvtLinestringGeometry(&simplifiedGeometry, tileBounds, false)...)
		}
	}

	return mvtGeometry
}

// emitPolygonRings emits the exterior ring (i==0, clockwise) and any hole rings
// (i>0, counter-clockwise) of a single polygon. A MultiPolygon is handled by
// calling this once per member polygon. The rings==0 fallback covers a bare ring
// that has no sub-geometries.
func (s *s57Tiler) emitPolygonRings(poly *gdal.Geometry, tileBounds m.Extrema) []uint32 {
	rings := poly.GeometryCount()
	if rings == 0 {
		return s.toMvtPolygonGeometry(poly, tileBounds, false)
	}
	out := make([]uint32, 0)
	for i := 0; i < rings; i++ {
		ring := poly.Geometry(i)
		out = append(out, s.toMvtPolygonGeometry(&ring, tileBounds, i > 0)...)
	}
	return out
}

func (s *s57Tiler) getMvtFeatureType(geometry *gdal.Geometry) *vectortile.Tile_GeomType {
	geomType := geometry.Type()
	var mvtGeomType vectortile.Tile_GeomType
	switch geomType {
	case gdal.GT_LineString, gdal.GT_LineString25D, gdal.GT_MultiLineString, gdal.GT_MultiLineString25D:
		mvtGeomType = vectortile.Tile_LINESTRING
	case gdal.GT_Polygon, gdal.GT_Polygon25D, gdal.GT_MultiPolygon, gdal.GT_MultiPolygon25D:
		mvtGeomType = vectortile.Tile_POLYGON
	case gdal.GT_Point, gdal.GT_Point25D, gdal.GT_MultiPoint, gdal.GT_MultiPoint25D:
		// GT_Point25D is what SOUNDG soundings become once SPLIT_MULTIPOINT is on.
		mvtGeomType = vectortile.Tile_POINT
	default:
		mvtGeomType = vectortile.Tile_UNKNOWN
	}
	return &mvtGeomType
}

// internalS57Fields are S-57 record-bookkeeping fields the GDAL driver exposes on
// every feature (record id, object label, version, agency, feature/spatial record
// pointers). They carry no charting meaning, bloat every tile, and FFPT_RIND in
// particular serializes as a malformed list string — so they are dropped rather
// than emitted as MVT tags.
var internalS57Fields = map[string]bool{
	"RCID": true, "PRIM": true, "GRUP": true, "OBJL": true, "RVER": true,
	"AGEN": true, "FIDN": true, "FIDS": true, "LNAM": true,
	"LNAM_REFS": true, "FFPT_RIND": true,
}

// decodeListString turns the GDAL wire format for a multi-valued S-57 attribute
// (e.g. a buoy's COLOUR, which the driver renders as "(2:1,4)") into a plain
// comma-separated value ("1,4"), so list attributes are emitted uniformly whether
// the driver types them as String/Integer/Real lists. It is defensive: anything
// not in that shape is returned unchanged, and an empty list yields "" (dropped by
// the caller's value != "" guard). FieldAsString is used rather than the typed list
// getters because the binding's FieldAsStringList dereferences a NULL pointer for an
// empty/unset list.
func decodeListString(s string) string {
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		if i := strings.Index(s, ":"); i >= 0 {
			return s[i+1 : len(s)-1]
		}
	}
	return s
}

func (s *s57Tiler) toMvtFeature(feature *gdal.Feature, tile m.TileID, tileBounds m.Extrema) *vectortile.Tile_Feature {
	geom := feature.Geometry()
	// A malformed record can carry a null geometry; bail before dereferencing it
	// (getMvtFeatureType/toMvtGeometry would otherwise call into a nil handle).
	if geom.IsNull() {
		return nil
	}
	mvtFeature := vectortile.Tile_Feature{}
	mvtFeature.Type = s.getMvtFeatureType(&geom)
	if *mvtFeature.Type != vectortile.Tile_UNKNOWN {
		// write tags
		for i := 0; i < feature.FieldCount(); i++ {
			fieldDef := feature.FieldDefinition(i)
			key := fieldDef.Name()
			if internalS57Fields[key] {
				continue
			}
			var value interface{}
			fieldType := fieldDef.Type()
			vt := VT_STRING
			if feature.IsFieldSet(i) {
				switch fieldType {
				case gdal.FT_StringList, gdal.FT_IntegerList, gdal.FT_Integer64List, gdal.FT_RealList:
					value = decodeListString(feature.FieldAsString(i))
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
		mvtFeature.Geometry = s.toMvtGeometry(*mvtFeature.Type, &geom, tile, tileBounds)
		return &mvtFeature
	}
	return nil
}

func includeFeatureInTile(feature gdal.Feature, tile m.TileID) bool {

	scale := m.Scale(tile)
	scaminIndex := feature.FieldIndex("SCAMIN")
	if scaminIndex >= 0 {
		scamin := feature.FieldAsFloat64(scaminIndex)
		if scamin != 0 && scamin < float64(scale) {
			return false
		}
	}
	scamaxIndex := feature.FieldIndex("SCAMAX")
	if scamaxIndex >= 0 {
		scamax := feature.FieldAsFloat64(scamaxIndex)
		if scamax != 0 && scamax > float64(scale) {
			return false
		}
	}

	return true
}

func (s *s57Tiler) GetFeatures(layer gdal.Layer, tile m.TileID, tileBounds m.Extrema) []*vectortile.Tile_Feature {

	features := make([]*vectortile.Tile_Feature, 0)
	b2 := m.Bounds(m.TileID{X: tile.X + 1, Y: tile.Y, Z: tile.Z})
	buffer := math.Abs(tileBounds.E-b2.E) / 4
	bounds := m.Extrema{N: tileBounds.N + buffer, S: tileBounds.S - buffer, W: tileBounds.W - buffer, E: tileBounds.E + buffer}

	layer.SetSpatialFilterRect(bounds.W, bounds.S, bounds.E, bounds.N)
	// Reset the cursor: the layer handle is reused across tiles now, so start each
	// scan from the beginning rather than wherever the previous tile left off.
	layer.ResetReading()

	ok := true

	for ok {
		feature := layer.NextFeature()
		if feature != nil {
			if includeFeatureInTile(*feature, tile) {
				mvtFeature := s.toMvtFeature(feature, tile, tileBounds)
				if mvtFeature != nil {
					features = append(features, mvtFeature)
				}
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
	// The M_COVR extent is already cached on the File (populated at discovery), so
	// read it directly. The previous version opened and immediately destroyed a
	// datasource here without ever using it — pure wasted I/O on every metadata write.
	layer, ok := file.Layers["M_COVR"]
	if !ok {
		return nil
	}
	return []float32{
		float32(layer.Bounds.MinX()),
		float32(layer.Bounds.MinY()),
		float32(layer.Bounds.MaxX()),
		float32(layer.Bounds.MaxY()),
	}
}

func (s *s57Tiler) GenerateMetaData(outPath string, dataset dataset.Dataset, file dataset.File, minZoom int, maxZoom int) error {
	path := filepath.Join(outPath, file.Id, "metadata.json")
	bounds := getBounds(file)
	// Fall back to the cell's catalog long-name when the dataset has no description,
	// so Freeboard shows a human-readable name instead of just the cell id.
	description := dataset.Description
	if description == "" {
		description = file.Title
	}
	metaData := charts.ChartMetaData{Id: file.Id, Name: file.Id, Description: description, Created: time.Now().UTC(), Type: "S-57", Format: "pbf", MinZoom: minZoom, MaxZoom: maxZoom, Bounds: bounds}

	out, err := json.Marshal(metaData)
	if err != nil {
		return fmt.Errorf("marshal metadata for %s: %w", file.Id, err)
	}
	// MkdirAll is a no-op when the directory already exists, so call it
	// unconditionally rather than guarding with a Stat.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (s *s57Tiler) GenerateTile(outPath string, file dataset.File, tile m.TileID) error {
	mvtTile := vectortile.Tile{}

	//allowedLayers := []string{"BOYLAT", "BOYCAR", "BOYINB", "BOYISD", "BOYSAW", "BOYSPP", "BCNLAT", "BCNCAR", "BCNISN", "BCNSAW", "BCNSPP", "LIGHTS", "DEPARE", "SEAARE", "COALNE", "RESARE", "UNSARE", "LNDARE", "BUAARE", "NAVLNE", "RECTRC", "CANALS"}

	bounds := m.Bounds(tile)
	tileEnvelope := gdal.Envelope{}
	tileEnvelope.SetMaxX(bounds.E)
	tileEnvelope.SetMaxY(bounds.N)
	tileEnvelope.SetMinX(bounds.W)
	tileEnvelope.SetMinY(bounds.S)

	// Reuse one open datasource for every layer and across every tile this tiler
	// generates (released by Close()), instead of reopening the S-57 cell each time.
	datasource := s.datasource(file.Path)

	for layerName, layer := range file.Layers {
		ln := layerName
		var version uint32 = 2
		var extent uint32 = TILE_EXTENT
		s.startLayer()
		mvtLayer := vectortile.Tile_Layer{Name: &ln, Version: &version, Extent: &extent}
		if layer.Bounds.Intersects(tileEnvelope) {
			l := datasource.LayerByName(layerName)
			// FeatureCount(false) returns (-1, false) for layers the S-57 driver
			// can't count without a full scan (e.g. split SOUNDG). Treat "unknown"
			// as "might have features" and let the spatial filter in GetFeatures
			// decide, rather than skipping the layer outright.
			c, ok := l.FeatureCount(false)
			if !ok || c > 0 {
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
		out, err := proto.Marshal(&mvtTile)
		if err != nil {
			return fmt.Errorf("marshal tile %s: %w", path, err)
		}
		// MkdirAll is a no-op when the directory already exists, so call it
		// unconditionally rather than guarding with a Stat.
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fmt.Errorf("create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(path, out, 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	} else {
		// Best-effort cleanup of a now-empty tile; a missing file is fine, any
		// other failure is surfaced so a stale invalid tile can't linger silently.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty tile %s: %w", path, err)
		}
	}
	return nil
}
