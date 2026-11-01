package fetch

import "fmt"

// HumanBytes is a size as a message to the user writes it: GiB with a decimal, or whole MiB.
func HumanBytes(n uint64) string {
	const gib = 1 << 30
	if n >= gib {
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	}
	return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
}
