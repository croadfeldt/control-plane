package cel_test

import (
	"github.com/dcm-project/control-plane/internal/cel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetNestedValue", func() {
	It("reads a top-level field", func() {
		val, err := cel.GetNestedValue(map[string]any{"name": "orders"}, "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("orders"))
	})

	It("reads a nested field", func() {
		m := map[string]any{
			"config": map[string]any{"host": "db.internal"},
		}
		val, err := cel.GetNestedValue(m, "config.host")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("db.internal"))
	})

	It("strips a leading spec. prefix", func() {
		m := map[string]any{
			"vcpu": map[string]any{"count": float64(4)},
		}
		val, err := cel.GetNestedValue(m, "spec.vcpu.count")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal(float64(4)))
	})

	It("reads through an array index", func() {
		m := map[string]any{
			"storage": map[string]any{
				"disks": []any{
					map[string]any{"name": "disk-a"},
				},
			},
		}
		val, err := cel.GetNestedValue(m, "storage.disks[0].name")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("disk-a"))
	})

	It("returns an indexed leaf value", func() {
		m := map[string]any{
			"items": []any{"first", "second"},
		}
		val, err := cel.GetNestedValue(m, "items[1]")
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("second"))
	})

	It("errors on missing paths", func() {
		_, err := cel.GetNestedValue(map[string]any{}, "missing.child")
		Expect(err).To(MatchError(ContainSubstring(`path segment "missing" not found`)))

		_, err = cel.GetNestedValue(map[string]any{"a": "b"}, "a.child")
		Expect(err).To(MatchError(ContainSubstring(`path segment "a" is not a map`)))
	})
})
