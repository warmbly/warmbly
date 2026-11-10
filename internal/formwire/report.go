package formwire

import (
	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/models"
)

// Report is one shared client report as the portal service renders it at /r/<token>: the brand, the numbers, and nothing that names the workspace's internals.
type Report struct {
	Brand  brandkit.Brand             `json:"brand"`
	Report models.ClientReportView    `json:"report"`
	Ranges []models.ClientReportRange `json:"ranges"`
}
