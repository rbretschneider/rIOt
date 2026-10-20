package collectors

import "time"

type ioSample struct {
	ID                  string
	At                  time.Time
	Rx, Tx, Read, Write uint64
}
type ioRate struct{ Rx, Tx, Read, Write float64 }

func ioRates(prev, cur ioSample) *ioRate {
	d := cur.At.Sub(prev.At).Seconds()
	if cur.ID != prev.ID || d < 1 || cur.Rx < prev.Rx || cur.Tx < prev.Tx || cur.Read < prev.Read || cur.Write < prev.Write {
		return nil
	}
	return &ioRate{float64(cur.Rx-prev.Rx) / d, float64(cur.Tx-prev.Tx) / d, float64(cur.Read-prev.Read) / d, float64(cur.Write-prev.Write) / d}
}
