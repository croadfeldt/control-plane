package service

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Nested Map Utilities", func() {
	Describe("stripSpecPrefix", func() {
		It("strips the spec. prefix", func() {
			Expect(stripSpecPrefix("spec.vcpu.count")).To(Equal("vcpu.count"))
			Expect(stripSpecPrefix("spec.memory.size_gb")).To(Equal("memory.size_gb"))
		})

		It("leaves paths without spec. prefix unchanged", func() {
			Expect(stripSpecPrefix("vcpu.count")).To(Equal("vcpu.count"))
			Expect(stripSpecPrefix("metadata.labels.tier")).To(Equal("metadata.labels.tier"))
		})

		It("returns empty string for bare spec. prefix", func() {
			Expect(stripSpecPrefix("spec.")).To(Equal(""))
		})
	})

	Describe("getNestedValue", func() {
		var m map[string]any

		BeforeEach(func() {
			m = map[string]any{
				"vcpu":   map[string]any{"count": float64(4), "frequency_ghz": 3.2},
				"memory": map[string]any{"size_gb": float64(16)},
				"metadata": map[string]any{
					"labels": map[string]any{"tier": "premium", "region": "us-east"},
				},
				"name":    "my-instance",
				"enabled": true,
			}
		})

		It("gets value at top level", func() {
			val, err := getNestedValue(m, "name")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal("my-instance"))
		})

		It("gets value at nested path", func() {
			val, err := getNestedValue(m, "spec.vcpu.count")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal(float64(4)))
		})

		It("gets deeply nested value", func() {
			val, err := getNestedValue(m, "spec.metadata.labels.tier")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal("premium"))
		})

		It("works without spec prefix", func() {
			val, err := getNestedValue(m, "vcpu.frequency_ghz")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal(3.2))
		})

		It("gets a boolean value", func() {
			val, err := getNestedValue(m, "enabled")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(BeTrue())
		})

		It("gets a nested map value", func() {
			val, err := getNestedValue(m, "spec.metadata.labels")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal(map[string]any{"tier": "premium", "region": "us-east"}))
		})

		It("returns error for empty path", func() {
			_, err := getNestedValue(m, "")
			Expect(err).To(HaveOccurred())
		})

		It("returns error for bare spec. prefix", func() {
			_, err := getNestedValue(m, "spec.")
			Expect(err).To(HaveOccurred())
		})

		It("returns error when leaf key does not exist", func() {
			_, err := getNestedValue(m, "spec.vcpu.threads")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("returns error when intermediate key does not exist", func() {
			_, err := getNestedValue(m, "spec.network.bandwidth")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("returns error when intermediate is not a map", func() {
			_, err := getNestedValue(m, "spec.vcpu.count.something")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a map"))
		})

		It("returns error when deeply nested intermediate is not a map", func() {
			_, err := getNestedValue(m, "spec.metadata.labels.tier.sub")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a map"))
		})
	})

	Describe("setNestedValue", func() {
		var m map[string]any

		BeforeEach(func() {
			m = make(map[string]any)
		})

		It("sets value at top level", func() {
			err := setNestedValue(m, "spec.vcpu", 4)
			Expect(err).ToNot(HaveOccurred())
			Expect(m["vcpu"]).To(Equal(4))
		})

		It("sets value at nested path", func() {
			err := setNestedValue(m, "spec.vcpu.count", 4)
			Expect(err).ToNot(HaveOccurred())
			vcpu, ok := m["vcpu"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(vcpu["count"]).To(Equal(4))
		})

		It("sets value into existing nested structure", func() {
			m["vcpu"] = map[string]any{"count": 2}
			err := setNestedValue(m, "spec.vcpu.count", 8)
			Expect(err).ToNot(HaveOccurred())
			vcpu := m["vcpu"].(map[string]any)
			Expect(vcpu["count"]).To(Equal(8))
		})

		It("creates deeply nested path", func() {
			err := setNestedValue(m, "spec.a.b.c.d", "deep")
			Expect(err).ToNot(HaveOccurred())
			a := m["a"].(map[string]any)
			b := a["b"].(map[string]any)
			c := b["c"].(map[string]any)
			Expect(c["d"]).To(Equal("deep"))
		})

		It("returns error when intermediate is not a map", func() {
			m["vcpu"] = "not-a-map"
			err := setNestedValue(m, "spec.vcpu.count", 4)
			Expect(err).To(HaveOccurred())
		})

		It("works without spec prefix", func() {
			err := setNestedValue(m, "vcpu.count", 4)
			Expect(err).ToNot(HaveOccurred())
			vcpu := m["vcpu"].(map[string]any)
			Expect(vcpu["count"]).To(Equal(4))
		})

		Context("with different value types", func() {
			It("sets a boolean value", func() {
				err := setNestedValue(m, "spec.gpu.enabled", true)
				Expect(err).ToNot(HaveOccurred())
				gpu := m["gpu"].(map[string]any)
				Expect(gpu["enabled"]).To(BeTrue())
			})

			It("sets a false boolean value", func() {
				err := setNestedValue(m, "spec.gpu.enabled", false)
				Expect(err).ToNot(HaveOccurred())
				gpu := m["gpu"].(map[string]any)
				Expect(gpu["enabled"]).To(BeFalse())
			})

			It("sets a float64 value", func() {
				err := setNestedValue(m, "spec.memory.size_gb", 16.5)
				Expect(err).ToNot(HaveOccurred())
				memory := m["memory"].(map[string]any)
				Expect(memory["size_gb"]).To(Equal(16.5))
			})

			It("sets a nil value", func() {
				err := setNestedValue(m, "spec.metadata.description", nil)
				Expect(err).ToNot(HaveOccurred())
				metadata := m["metadata"].(map[string]any)
				Expect(metadata["description"]).To(BeNil())
			})

			It("sets a slice value", func() {
				tags := []string{"prod", "gpu", "high-mem"}
				err := setNestedValue(m, "spec.metadata.tags", tags)
				Expect(err).ToNot(HaveOccurred())
				metadata := m["metadata"].(map[string]any)
				Expect(metadata["tags"]).To(Equal(tags))
			})

			It("sets a nested map value", func() {
				labels := map[string]any{"tier": "premium", "region": "us-east"}
				err := setNestedValue(m, "spec.metadata.labels", labels)
				Expect(err).ToNot(HaveOccurred())
				metadata := m["metadata"].(map[string]any)
				Expect(metadata["labels"]).To(Equal(labels))
			})
		})

		Context("with multiple fields set on the same map", func() {
			It("sets sibling fields without overwriting each other", func() {
				Expect(setNestedValue(m, "spec.vcpu.count", 4)).To(Succeed())
				Expect(setNestedValue(m, "spec.vcpu.frequency_ghz", 3.2)).To(Succeed())
				Expect(setNestedValue(m, "spec.vcpu.hyperthreading", true)).To(Succeed())

				vcpu := m["vcpu"].(map[string]any)
				Expect(vcpu).To(HaveLen(3))
				Expect(vcpu["count"]).To(Equal(4))
				Expect(vcpu["frequency_ghz"]).To(Equal(3.2))
				Expect(vcpu["hyperthreading"]).To(BeTrue())
			})

			It("sets fields across different subtrees", func() {
				Expect(setNestedValue(m, "spec.vcpu.count", 8)).To(Succeed())
				Expect(setNestedValue(m, "spec.memory.size_gb", 32.0)).To(Succeed())
				Expect(setNestedValue(m, "spec.gpu.enabled", false)).To(Succeed())

				Expect(m["vcpu"].(map[string]any)["count"]).To(Equal(8))
				Expect(m["memory"].(map[string]any)["size_gb"]).To(Equal(32.0))
				Expect(m["gpu"].(map[string]any)["enabled"]).To(BeFalse())
			})
		})

		Context("with error cases", func() {
			It("returns error for empty path and sets nothing", func() {
				err := setNestedValue(m, "", "should-not-be-set")
				Expect(err).To(HaveOccurred())
				Expect(m).To(BeEmpty())
			})

			It("returns error when an integer intermediate blocks traversal", func() {
				m["vcpu"] = 42
				err := setNestedValue(m, "spec.vcpu.count", 4)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("vcpu"))
			})

			It("returns error when a boolean intermediate blocks traversal", func() {
				m["gpu"] = true
				err := setNestedValue(m, "spec.gpu.model.name", "A100")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("gpu"))
			})

			It("returns error when a deeply nested intermediate is not a map", func() {
				m["a"] = map[string]any{"b": "leaf"}
				err := setNestedValue(m, "spec.a.b.c.d", "value")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("a.b"))
			})
		})

		It("overwrites an existing value with a different type", func() {
			Expect(setNestedValue(m, "spec.vcpu.count", 4)).To(Succeed())
			Expect(setNestedValue(m, "spec.vcpu.count", "four")).To(Succeed())
			vcpu := m["vcpu"].(map[string]any)
			Expect(vcpu["count"]).To(Equal("four"))
		})

		It("handles a single-segment path without spec prefix", func() {
			err := setNestedValue(m, "name", "my-instance")
			Expect(err).ToNot(HaveOccurred())
			Expect(m["name"]).To(Equal("my-instance"))
		})
	})

	Describe("array index paths (FLPATH-4634)", func() {
		It("builds a real disks array from indexed field paths", func() {
			// FLPATH-4634 catalog field defaults (kubevirt VM create payload shape)
			m := map[string]any{}
			Expect(setNestedValue(m, "metadata.name", "test-vm")).To(Succeed())
			Expect(setNestedValue(m, "guest_os.type", "linux")).To(Succeed())
			Expect(setNestedValue(m, "vcpu.count", float64(2))).To(Succeed())
			Expect(setNestedValue(m, "memory.size", "4Gi")).To(Succeed())
			Expect(setNestedValue(m, "storage.disks[0].name", "boot")).To(Succeed())
			Expect(setNestedValue(m, "storage.disks[0].capacity", "10GB")).To(Succeed())

			Expect(m).NotTo(HaveKey("disks[0]"))
			Expect(m["storage"]).NotTo(HaveKey("disks[0]"))
			Expect(m).To(Equal(map[string]any{
				"metadata": map[string]any{"name": "test-vm"},
				"guest_os": map[string]any{"type": "linux"},
				"vcpu":     map[string]any{"count": float64(2)},
				"memory":   map[string]any{"size": "4Gi"},
				"storage": map[string]any{
					"disks": []any{
						map[string]any{"name": "boot", "capacity": "10GB"},
					},
				},
			}))
		})

		It("supports multiple array indices", func() {
			m := map[string]any{}
			Expect(setNestedValue(m, "storage.disks[0].name", "boot")).To(Succeed())
			Expect(setNestedValue(m, "storage.disks[1].name", "data")).To(Succeed())

			disks := m["storage"].(map[string]any)["disks"].([]any)
			Expect(disks).To(HaveLen(2))
			Expect(disks[0].(map[string]any)["name"]).To(Equal("boot"))
			Expect(disks[1].(map[string]any)["name"]).To(Equal("data"))
		})

		It("gets values via indexed paths", func() {
			m := map[string]any{}
			Expect(setNestedValue(m, "storage.disks[0].name", "boot")).To(Succeed())

			val, err := getNestedValue(m, "storage.disks[0].name")
			Expect(err).NotTo(HaveOccurred())
			Expect(val).To(Equal("boot"))
		})

		It("sets a whole element when the last segment is indexed", func() {
			m := map[string]any{}
			Expect(setNestedValue(m, "storage.disks[0]", map[string]any{"name": "boot"})).To(Succeed())

			disks := m["storage"].(map[string]any)["disks"].([]any)
			Expect(disks[0]).To(Equal(map[string]any{"name": "boot"}))
		})

		It("handles sparse indices, holes on get, growth, and non-array clash", func() {
			m := map[string]any{}
			Expect(setNestedValue(m, "storage.disks[1].name", "data")).To(Succeed())

			disks := m["storage"].(map[string]any)["disks"].([]any)
			Expect(disks).To(HaveLen(2))
			Expect(disks[0]).To(BeNil())
			Expect(disks[1].(map[string]any)["name"]).To(Equal("data"))

			_, err := getNestedValue(m, "storage.disks[0].name")
			Expect(err).To(MatchError(ContainSubstring("not a map")))

			val, err := getNestedValue(m, "storage.disks[0]")
			Expect(err).NotTo(HaveOccurred())
			Expect(val).To(BeNil())

			Expect(setNestedValue(m, "storage.volumes[0].name", "v0")).To(Succeed())
			Expect(setNestedValue(m, "storage.volumes[2].name", "v2")).To(Succeed())
			volumes := m["storage"].(map[string]any)["volumes"].([]any)
			Expect(volumes).To(HaveLen(3))
			Expect(volumes[0].(map[string]any)["name"]).To(Equal("v0"))
			Expect(volumes[1]).To(BeNil())
			Expect(volumes[2].(map[string]any)["name"]).To(Equal("v2"))

			clash := map[string]any{"disks": map[string]any{"name": "not-an-array"}}
			Expect(setNestedValue(clash, "disks[0].x", "y")).To(MatchError(ContainSubstring("not an array")))
		})

		DescribeTable("rejects malformed indexed segments",
			func(path string) {
				err := setNestedValue(map[string]any{}, path, "x")
				Expect(err).To(HaveOccurred())
			},
			Entry("brackets reversed", "storage.disks][0.name"),
			Entry("non-numeric index", "storage.disks[abc].name"),
			Entry("unclosed bracket", "storage.disks[0.name"),
			Entry("nested indices", "storage.disks[0][1].name"),
			Entry("empty name", "storage.[0].name"),
			Entry("index too large", "storage.disks[1025].name"),
		)
	})
})
