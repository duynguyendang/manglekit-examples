// testdata/clean.go must trigger ZERO signals (negative fixture; the Go
// tooling ignores testdata/, so this file is never compiled).
package clean

func Add(a, b int) int {
	return a + b
}
