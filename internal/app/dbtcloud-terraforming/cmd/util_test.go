package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSortedKeys locks in sortedKeys' deterministic-ordering contract: given
// the same map contents, it must always return the same key order,
// regardless of Go's randomized map iteration order. This is what
// generate.go/import.go rely on to produce stable dbtcloud_environment_variable
// output across runs.
func TestSortedKeys(t *testing.T) {
	t.Run("string keys are sorted ascending", func(t *testing.T) {
		m := map[string]any{
			"DBT_TEST_BPER": 1,
			"DBT_NEWONE":    2,
			"DBT_ALPHA":     3,
			"DBT_API":       4,
		}
		got := sortedKeys(m)
		assert.Equal(t, []string{"DBT_ALPHA", "DBT_API", "DBT_NEWONE", "DBT_TEST_BPER"}, got)
	})

	t.Run("int keys are sorted ascending", func(t *testing.T) {
		m := map[int]any{71: "a", 5: "b", 100: "c"}
		got := sortedKeys(m)
		assert.Equal(t, []int{5, 71, 100}, got)
	})

	t.Run("empty map returns empty slice", func(t *testing.T) {
		m := map[string]any{}
		got := sortedKeys(m)
		assert.Equal(t, []string{}, got)
	})

	t.Run("result is stable across repeated calls on the same map", func(t *testing.T) {
		m := map[string]any{"c": 1, "a": 2, "b": 3, "d": 4, "e": 5}
		first := sortedKeys(m)
		for range 20 {
			assert.Equal(t, first, sortedKeys(m))
		}
	})
}
