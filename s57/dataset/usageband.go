package dataset

import (
	"github.com/lukeroth/gdal"
)

// UsageBand returns the S-57 navigational purpose (usage band) of a cell:
// 1=Overview, 2=General, 3=Coastal, 4=Approach, 5=Harbour, 6=Berthing. It reads the
// authoritative DSID_INTU value from the cell's DSID record. The DSID layer is
// geometry-less and therefore excluded from File.Layers, so we open the datasource
// and scan layers by index. Falls back to the navigational-purpose digit in the
// cell name (3rd character, e.g. US"4"WA1JJ) and returns 0 when unknown.
func UsageBand(file File) int {
	ds := gdal.OpenDataSource(file.Path, 0)
	defer ds.Destroy()
	for i := 0; i < ds.LayerCount(); i++ {
		layer := ds.LayerByIndex(i)
		if layer.Name() != "DSID" {
			continue
		}
		layer.ResetReading()
		feat := layer.NextFeature()
		if feat != nil {
			band := 0
			if idx := feat.FieldIndex("DSID_INTU"); idx >= 0 && feat.IsFieldSet(idx) {
				band = int(feat.FieldAsInteger64(idx))
			}
			feat.Destroy()
			if band >= 1 && band <= 6 {
				return band
			}
		}
		break
	}
	if len(file.Id) >= 3 && file.Id[2] >= '1' && file.Id[2] <= '6' {
		return int(file.Id[2] - '0')
	}
	return 0
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
