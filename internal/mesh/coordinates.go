package mesh

import "math"

type Coordinates struct {
	Lat  float64 // Latitude
	Long float64 // Longitude
}

// TODO: Change this to use distance between two points on the earth's surface
// Distance calculation based on x and y instead of lat and long
func (c Coordinates) DistanceTo(other Coordinates) float64 {
	return math.Sqrt(math.Pow(c.Lat-other.Lat, 2) + math.Pow(c.Long-other.Long, 2))
}

func (c Coordinates) Equals(other Coordinates) bool {
	return c.Lat == other.Lat && c.Long == other.Long
}

func CreateCoordinates(lat float64, long float64) Coordinates {
	return Coordinates{Lat: lat, Long: long}
}
