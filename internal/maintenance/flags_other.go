//go:build !linux

package maintenance

// ReconcileDietPiFlags does nothing off Linux: DietPi runs only there.
func ReconcileDietPiFlags(string) string { return "" }
