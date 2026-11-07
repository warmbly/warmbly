package leadfinder

import (
	"errors"
	"math"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	earthRadiusM = 6371000.0
	// maxPolygonPoints bounds a drawn shape.
	maxPolygonPoints = 200
	// maxRadiusM is the widest circle any provider is asked about.
	maxRadiusM = 50000
)

// NormalizeArea validates a requested area and fills in its bounding box.
func NormalizeArea(a models.LeadArea) (models.LeadArea, error) {
	switch a.Kind {
	case models.LeadAreaCircle:
		if a.Center == nil || !validPoint(*a.Center) {
			return a, errors.New("a circle needs a center on the map")
		}
		if a.RadiusM < 50 || a.RadiusM > maxRadiusM {
			return a, errors.New("a circle's radius must be between 50 m and 50 km")
		}
		dLat := a.RadiusM / earthRadiusM * 180 / math.Pi
		dLng := dLat / math.Max(math.Cos(a.Center.Lat*math.Pi/180), 0.01)
		a.Bounds = models.LeadBounds{
			South: clampLat(a.Center.Lat - dLat), North: clampLat(a.Center.Lat + dLat),
			West: wrapLng(a.Center.Lng - dLng), East: wrapLng(a.Center.Lng + dLng),
		}
		a.Polygon = nil
	case models.LeadAreaPolygon:
		if len(a.Polygon) < 3 || len(a.Polygon) > maxPolygonPoints {
			return a, errors.New("a drawn area needs between 3 and 200 points")
		}
		b := models.LeadBounds{South: 90, North: -90, West: 180, East: -180}
		for _, p := range a.Polygon {
			if !validPoint(p) {
				return a, errors.New("a drawn area has a point off the map")
			}
			b.South, b.North = math.Min(b.South, p.Lat), math.Max(b.North, p.Lat)
			b.West, b.East = math.Min(b.West, p.Lng), math.Max(b.East, p.Lng)
		}
		a.Bounds = b
		a.Center, a.RadiusM = nil, 0
	default:
		a.Kind = models.LeadAreaBBox
		b := a.Bounds
		if b.South >= b.North || b.South < -90 || b.North > 90 || b.West < -180 || b.West > 180 || b.East < -180 || b.East > 180 {
			return a, errors.New("the map area is not a valid rectangle")
		}
		a.Center, a.RadiusM, a.Polygon = nil, 0, nil
	}
	return a, nil
}

func validPoint(p models.LatLng) bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

func clampLat(v float64) float64 { return math.Max(-89.9, math.Min(89.9, v)) }

func wrapLng(v float64) float64 {
	for v > 180 {
		v -= 360
	}
	for v < -180 {
		v += 360
	}
	return v
}

// lngSpan is the bounds' width in degrees, across the antimeridian when needed.
func lngSpan(b models.LeadBounds) float64 {
	if b.West <= b.East {
		return b.East - b.West
	}
	return 360 - b.West + b.East
}

// Center is the middle of the bounds.
func Center(b models.LeadBounds) models.LatLng {
	return models.LatLng{Lat: (b.South + b.North) / 2, Lng: wrapLng(b.West + lngSpan(b)/2)}
}

// DiagonalM is the distance across the bounds, corner to corner.
func DiagonalM(b models.LeadBounds) float64 {
	return Distance(models.LatLng{Lat: b.South, Lng: b.West}, models.LatLng{Lat: b.North, Lng: b.West + lngSpan(b)})
}

// AreaKM2 approximates the bounds' surface.
func AreaKM2(b models.LeadBounds) float64 {
	midLat := (b.South + b.North) / 2 * math.Pi / 180
	h := (b.North - b.South) * math.Pi / 180 * earthRadiusM
	w := lngSpan(b) * math.Pi / 180 * earthRadiusM * math.Cos(midLat)
	return h * w / 1e6
}

// Distance is the great-circle distance between two points in meters.
func Distance(a, b models.LatLng) float64 {
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dLat := la2 - la1
	dLng := (b.Lng - a.Lng) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Contains reports whether a point is inside the area.
func Contains(a models.LeadArea, p models.LatLng) bool {
	switch a.Kind {
	case models.LeadAreaCircle:
		return a.Center != nil && Distance(*a.Center, p) <= a.RadiusM
	case models.LeadAreaPolygon:
		return inPolygon(a.Polygon, p)
	}
	b := a.Bounds
	if p.Lat < b.South || p.Lat > b.North {
		return false
	}
	if b.West <= b.East {
		return p.Lng >= b.West && p.Lng <= b.East
	}
	return p.Lng >= b.West || p.Lng <= b.East
}

// inPolygon is the even-odd ray cast.
func inPolygon(poly []models.LatLng, p models.LatLng) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) &&
			p.Lng < (b.Lng-a.Lng)*(p.Lat-a.Lat)/(b.Lat-a.Lat)+a.Lng {
			in = !in
		}
	}
	return in
}
