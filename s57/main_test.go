package s57

import (
	"os"
	"testing"

	"github.com/wdantuma/s57-tiler/s57/dataset"
)

// TestMain configures the GDAL reader options before any test opens a datasource
// (e.g. SOUNDG splitting), now that dataset.ConfigureGDAL replaces the package init().
func TestMain(m *testing.M) {
	dataset.ConfigureGDAL()
	os.Exit(m.Run())
}
