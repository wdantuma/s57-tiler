package mercantile

//port of https://github.com/mapbox/mercantile

import "math"

// Extrema structure (Bounding Box)
type Extrema struct {
	W float64
	E float64
	N float64
	S float64
}

// TileID represents the id of the tile.
type TileID struct {
	X int64
	Y int64
	Z uint64
}

// Point represents a point in space.
type Point struct {
	X float64
	Y float64
}

// Returns the upper left (lon, lat) of a tile.
func Ul(tileid TileID) Point {
	n := math.Pow(2.0, float64(tileid.Z))
	lon_deg := float64(tileid.X)/n*360.0 - 180.0
	lat_rad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(tileid.Y)/n)))
	lat_deg := (180.0 / math.Pi) * lat_rad
	return Point{lon_deg, lat_deg}
}

// Returns the (lon, lat) bounding box of a tile.
func Bounds(tileid TileID) Extrema {
	a := Ul(tileid)
	b := Ul(TileID{tileid.X + 1, tileid.Y + 1, tileid.Z})
	return Extrema{W: a.X, S: b.Y, E: b.X, N: a.Y}
}

func Scale(tileid TileID) int32 {
	switch tileid.Z {
	case 0:
		return 500000000
	case 1:
		return 250000000
	case 2:
		return 150000000
	case 3:
		return 70000000
	case 4:
		return 35000000
	case 5:
		return 15000000
	case 6:
		return 10000000
	case 7:
		return 4000000
	case 8:
		return 2000000
	case 9:
		return 1000000
	case 10:
		return 500000
	case 11:
		return 250000
	case 12:
		return 150000
	case 13:
		return 70000
	case 14:
		return 35000
	case 15:
		return 24000
	case 16:
		return 15000
	case 17:
		return 8000
	case 18:
		return 4000
	case 19:
		return 2000
	case 20:
		return 1000
	case 21:
		return 500
	case 22:
		return 250
	case 23:
		return 150
	default:
		return 0
	}
}

// ZoomForScale returns the finest (highest) web-map zoom whose mapped scale
// denominator (see Scale) is still >= the given chart compilation-scale
// denominator. It is the inverse of Scale: a 1:2000 chart (scaleDenominator =
// 2000) resolves to the zoom its detail is authored for. It is driven off the
// same Scale table so the mapping lives in exactly one place.
//
// Scale denominators decrease as zoom increases, so we walk zoom upward and stop
// at the first level whose Scale is <= the requested denominator. A non-positive
// denominator (e.g. missing DSPM_CSCL) yields 0; a denominator finer than the
// table's deepest entry clamps to that deepest zoom.
func ZoomForScale(scaleDenominator int32) uint64 {
	if scaleDenominator <= 0 {
		return 0
	}
	const maxTableZoom = 23 // deepest zoom Scale defines
	for z := uint64(0); z <= maxTableZoom; z++ {
		if Scale(TileID{Z: z}) <= scaleDenominator {
			return z
		}
	}
	return maxTableZoom
}

// Returns the (x, y, z) tile.
func Tile(lng float64, lat float64, zoom int) TileID {
	lat = lat * (math.Pi / 180.0)
	n := math.Pow(2.0, float64(zoom))
	xtile := int(math.Floor((lng + 180.0) / 360.0 * n))
	ytile := int(math.Floor((1.0 - math.Log(math.Tan(lat)+(1.0/math.Cos(lat)))/math.Pi) / 2.0 * n))
	// Clamp to the valid [0, 2^zoom) grid. At the antimeridian (lng=180) the x
	// math lands exactly on 2^zoom (one past the last column), and latitudes past
	// the Web-Mercator limit drive y out of range; either would make callers like
	// GetTilesForBounds iterate off-grid and emit invalid tiles.
	max := int(n) - 1
	xtile = clampTileIndex(xtile, max)
	ytile = clampTileIndex(ytile, max)
	return TileID{int64(xtile), int64(ytile), uint64(zoom)}
}

func clampTileIndex(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}
