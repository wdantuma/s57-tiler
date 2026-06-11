package mercantile

import "testing"

// The tilestr parsers are fed strings from filenames/URLs, so malformed input
// must not panic (it previously indexed vals[1]/vals[2] without a length check,
// and TileFromString dereferenced a nil slice when no delimiter matched).

func TestStrtileShortInputDoesNotPanic(t *testing.T) {
	if got := Strtile("1/2"); got != (TileID{}) {
		t.Errorf("Strtile(\"1/2\") = %+v, want zero TileID", got)
	}
	if got := Strtile(""); got != (TileID{}) {
		t.Errorf("Strtile(\"\") = %+v, want zero TileID", got)
	}
}

func TestStrtileValidRoundTrips(t *testing.T) {
	if got := Strtile("3/5/7"); got != (TileID{X: 3, Y: 5, Z: 7}) {
		t.Errorf("Strtile(\"3/5/7\") = %+v, want {3,5,7}", got)
	}
}

func TestTileFromStringNoDelimiterDoesNotPanic(t *testing.T) {
	if got := TileFromString("garbage"); got != (TileID{}) {
		t.Errorf("TileFromString(\"garbage\") = %+v, want zero TileID", got)
	}
}

func TestTileFromStringValid(t *testing.T) {
	if got := TileFromString("3-5-7"); got != (TileID{X: 3, Y: 5, Z: 7}) {
		t.Errorf("TileFromString(\"3-5-7\") = %+v, want {3,5,7}", got)
	}
}
