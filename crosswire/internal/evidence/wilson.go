package evidence

import "math"

// Interval is a 95 % Wilson score interval for k successes in n trials.
type Interval struct {
	K     int     `json:"k"`
	N     int     `json:"n"`
	Rate  float64 `json:"rate"`
	Lower float64 `json:"lower"`
	Upper float64 `json:"upper"`
}

// Wilson computes the interval. For n = 0 every field is zero.
func Wilson(k, n int) Interval {
	if n <= 0 {
		return Interval{}
	}
	const z = 1.959964
	p := float64(k) / float64(n)
	nn := float64(n)
	denom := 1 + z*z/nn
	centre := (p + z*z/(2*nn)) / denom
	half := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / denom
	return Interval{
		K:     k,
		N:     n,
		Rate:  round3(p),
		Lower: round3(math.Max(0, centre-half)),
		Upper: round3(math.Min(1, centre+half)),
	}
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
