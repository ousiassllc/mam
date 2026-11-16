package modelcatalog

import "github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"

// UnitPrice is the price of one result of the model: its basis and, when the price is known, the USD.
func UnitPrice(info Info, media, resolution string, seconds float64) (*float64, string) {
	zero := 0.0
	switch info.Price.Basis {
	case BasisFree:
		return &zero, BasisFree
	case BasisPriced:
		if media == gentestrepo.MediaImage && info.Price.PerImageUSD != nil {
			v := *info.Price.PerImageUSD
			return &v, BasisPriced
		}
		if per, ok := info.Price.PerSecondUSD[resolution]; ok && media == gentestrepo.MediaVideo {
			v := per * seconds
			return &v, BasisPriced
		}
	}
	return nil, BasisUnknown
}
