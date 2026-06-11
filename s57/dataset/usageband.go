package dataset

import (
	"github.com/lukeroth/gdal"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// readDSID reads the cell's DSID record from an already-open datasource and
// returns the usage band (DSID_INTU), the compilation-scale denominator
// (DSPM_CSCL), and whether a DSID record was found. The values are cached on the
// File at discovery so the zoom derivation never has to reopen the cell. The DSID
// layer is geometry-less and therefore excluded from File.Layers, so we scan
// layers by index.
func readDSID(ds gdal.DataSource) (intu int, cscl int, found bool) {
	for i := 0; i < ds.LayerCount(); i++ {
		layer := ds.LayerByIndex(i)
		if layer.Name() != "DSID" {
			continue
		}
		found = true
		layer.ResetReading()
		feat := layer.NextFeature()
		if feat != nil {
			if idx := feat.FieldIndex("DSID_INTU"); idx >= 0 && feat.IsFieldSet(idx) {
				intu = int(feat.FieldAsInteger64(idx))
			}
			if idx := feat.FieldIndex("DSPM_CSCL"); idx >= 0 && feat.IsFieldSet(idx) {
				cscl = int(feat.FieldAsInteger64(idx))
			}
			feat.Destroy()
		}
		break
	}
	return intu, cscl, found
}

// resolveBand maps a cell's DSID_INTU and id to a standard S-57 usage band
// (1..6), falling back to the navigational-purpose digit in the cell name (3rd
// character, e.g. US"4"WA1JJ) and returning 0 when neither yields 1..6 (e.g. an
// Inland-ENC band such as 7).
func resolveBand(intu int, id string) int {
	if intu >= 1 && intu <= 6 {
		return intu
	}
	if len(id) >= 3 && id[2] >= '1' && id[2] <= '6' {
		return int(id[2] - '0')
	}
	return 0
}

// UsageBand returns the S-57 navigational purpose (usage band) of a cell:
// 1=Overview, 2=General, 3=Coastal, 4=Approach, 5=Harbour, 6=Berthing. It reads
// the authoritative DSID_INTU value from the cell's DSID record, falls back to
// the navigational-purpose digit in the cell name, and returns 0 when unknown.
func UsageBand(file File) int {
	return resolveBand(file.Intu, file.Id)
}

// BandZoom maps a usage band to a default web-map zoom range. The values are a
// starting point, chosen so band 4 (Approach) stays close to the historical 9–14
// default; an unknown band (0) keeps that default.
func BandZoom(band int) (minZoom, maxZoom int) {
	switch band {
	case 1: // Overview
		return 5, 9
	case 2: // General
		return 7, 11
	case 3: // Coastal
		return 9, 12
	case 4: // Approach
		return 10, 14
	case 5: // Harbour
		return 12, 16
	case 6: // Berthing
		return 13, 17
	default:
		return 9, 14
	}
}

// ZoomRange is the web-map zoom range a cell is tiled to by default: from a
// sensible coarse Min up to Max, the finest zoom matching the cell's native
// detail. There is no artificial cap — a chart is tiled to the resolution it
// actually contains, so nothing relies on the consuming client overzooming.
// Users can still narrow this with -minzoom/-maxzoom.
type ZoomRange struct {
	Min, Max int
}

// CellZoomRange returns a cell's default zoom range from its cached DSID values
// (read once at discovery). The minimum comes from the usage band (overview
// charts start coarse, harbour charts start fine); for an inland/unknown band it
// is a fixed floor. The maximum is the cell's native zoom — the finest level
// matching its compilation scale (DSPM_CSCL) — and is never capped below the band
// default. So a 1:2000 inland chart tiles up to z19 and a 1:20000 approach chart
// up to z16.
func CellZoomRange(file File) ZoomRange {
	const inlandFloor = 12 // coarse floor for inland/large-scale cells (no usage band)
	cscl := file.Cscl
	band := resolveBand(file.Intu, file.Id)

	var minZ, maxZ int
	switch {
	case band >= 1 && band <= 6:
		minZ, maxZ = BandZoom(band) // marine: usage-band min and max
	case cscl > 0:
		minZ, maxZ = inlandFloor, int(m.ZoomForScale(int32(cscl)))
	default:
		minZ, maxZ = BandZoom(0) // no band, no scale -> historical 9..14
	}

	// Extend the max to the cell's native compilation scale when that is finer,
	// so a detailed chart isn't tiled below the resolution it actually contains.
	if cscl > 0 {
		if native := int(m.ZoomForScale(int32(cscl))); native > maxZ {
			maxZ = native
		}
	}
	if minZ > maxZ {
		minZ = maxZ
	}
	return ZoomRange{Min: minZ, Max: maxZ}
}

// CellZoom returns a cell's default web-map zoom range; see CellZoomRange.
func CellZoom(file File) (minZoom, maxZoom int) {
	r := CellZoomRange(file)
	return r.Min, r.Max
}
