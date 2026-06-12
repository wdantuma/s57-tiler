package dataset

import (
	"os"
	"testing"
)

// TestMain configures the GDAL reader options before any test opens a datasource,
// now that ConfigureGDAL replaces the package init().
func TestMain(m *testing.M) {
	ConfigureGDAL()
	os.Exit(m.Run())
}
