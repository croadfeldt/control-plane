package service

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test suite is registered in other test files - don't register again

var _ = Describe("ConstraintContext", func() {
	var constraintCtx *ConstraintContext

	BeforeEach(func() {
		constraintCtx = NewConstraintContext()
	})

	Describe("MergeConstraints", func() {
		It("stores first constraints for a field", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")

			Expect(err).NotTo(HaveOccurred())
			constraints := constraintCtx.GetConstraintsMap()
			Expect(constraints).To(HaveKey("region"))
		})

		It("allows tightening minimum (increasing)", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"minimum": float64(1),
					"maximum": float64(8),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"minimum": float64(2),
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			// Minimum should now be 2
			constraints := constraintCtx.GetConstraintsMap()
			cpuConstraint := constraints["cpu_count"].(map[string]any)
			Expect(cpuConstraint["minimum"]).To(Equal(float64(2)))
			Expect(cpuConstraint["maximum"]).To(Equal(float64(8)))
		})

		It("allows tightening maximum (decreasing)", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"maximum": float64(8),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"maximum": float64(4),
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			constraints := constraintCtx.GetConstraintsMap()
			cpuConstraint := constraints["cpu_count"].(map[string]any)
			Expect(cpuConstraint["maximum"]).To(Equal(float64(4)))
		})

		It("rejects loosening minimum (decreasing)", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"minimum": float64(4),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"minimum": float64(2),
				},
			}, "policy-2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("loosen"))
		})

		It("rejects loosening maximum (increasing)", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"maximum": float64(4),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"maximum": float64(8),
				},
			}, "policy-2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("loosen"))
		})

		It("intersects enum values", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"enum": []any{"us-east-1", "us-west-2", "eu-west-1"},
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"enum": []any{"us-east-1", "eu-west-1", "ap-south-1"},
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			constraints := constraintCtx.GetConstraintsMap()
			regionConstraint := constraints["region"].(map[string]any)
			Expect(regionConstraint["enum"]).To(ConsistOf("us-east-1", "eu-west-1"))
		})

		It("rejects empty enum intersection", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"enum": []any{"us-east-1"},
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"enum": []any{"eu-west-1"},
				},
			}, "policy-2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty"))
		})

		It("allows identical const values", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects different const values", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-west-2",
				},
			}, "policy-2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("const"))
		})

		It("allows tightening minLength", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"name": map[string]any{
					"minLength": float64(3),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"name": map[string]any{
					"minLength": float64(5),
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			constraints := constraintCtx.GetConstraintsMap()
			nameConstraint := constraints["name"].(map[string]any)
			Expect(nameConstraint["minLength"]).To(Equal(float64(5)))
		})

		It("allows tightening maxLength", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"name": map[string]any{
					"maxLength": float64(100),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.MergeConstraints(map[string]any{
				"name": map[string]any{
					"maxLength": float64(50),
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			constraints := constraintCtx.GetConstraintsMap()
			nameConstraint := constraints["name"].(map[string]any)
			Expect(nameConstraint["maxLength"]).To(Equal(float64(50)))
		})

		It("validates multipleOf compatibility", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"multipleOf": float64(2),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// 4 is a multiple of 2 — should succeed
			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"multipleOf": float64(4),
				},
			}, "policy-2")
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects incompatible multipleOf", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"multipleOf": float64(4),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// 3 is not a multiple of 4
			err = constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"multipleOf": float64(3),
				},
			}, "policy-2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("multipleOf"))
		})
	})

	Describe("ValidatePatch", func() {
		It("validates patch value against const constraint", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// Valid patch
			violations := constraintCtx.ValidatePatch(map[string]any{
				"region": "us-east-1",
			})
			Expect(violations).To(BeEmpty())

			// Invalid patch
			violations = constraintCtx.ValidatePatch(map[string]any{
				"region": "us-west-2",
			})
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].FieldPath).To(Equal("region"))
			Expect(violations[0].SetByPolicy).To(Equal("policy-1"))
		})

		It("validates patch value against minimum/maximum constraints", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"cpu_count": map[string]any{
					"minimum": float64(1),
					"maximum": float64(8),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// Valid value
			violations := constraintCtx.ValidatePatch(map[string]any{
				"cpu_count": float64(4),
			})
			Expect(violations).To(BeEmpty())

			// Too low
			violations = constraintCtx.ValidatePatch(map[string]any{
				"cpu_count": float64(0),
			})
			Expect(violations).To(HaveLen(1))

			// Too high
			violations = constraintCtx.ValidatePatch(map[string]any{
				"cpu_count": float64(16),
			})
			Expect(violations).To(HaveLen(1))
		})

		It("validates patch value against enum constraint", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"enum": []any{"us-east-1", "us-west-2"},
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// Valid value
			violations := constraintCtx.ValidatePatch(map[string]any{
				"region": "us-east-1",
			})
			Expect(violations).To(BeEmpty())

			// Invalid value
			violations = constraintCtx.ValidatePatch(map[string]any{
				"region": "eu-west-1",
			})
			Expect(violations).To(HaveLen(1))
		})

		It("allows unconstrained fields in patch", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// instance_type has no constraint — should be fine
			violations := constraintCtx.ValidatePatch(map[string]any{
				"instance_type": "t3.large",
			})
			Expect(violations).To(BeEmpty())
		})

		It("validates nested patch fields", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"compute.cpu_count": map[string]any{
					"minimum": float64(2),
					"maximum": float64(8),
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			// Valid nested value
			violations := constraintCtx.ValidatePatch(map[string]any{
				"compute": map[string]any{
					"cpu_count": float64(4),
				},
			})
			Expect(violations).To(BeEmpty())

			// Invalid nested value
			violations = constraintCtx.ValidatePatch(map[string]any{
				"compute": map[string]any{
					"cpu_count": float64(16),
				},
			})
			Expect(violations).To(HaveLen(1))
			Expect(violations[0].FieldPath).To(Equal("compute.cpu_count"))
		})
	})

	Describe("GetConstraintsMap", func() {
		It("returns nil when no constraints exist", func() {
			Expect(constraintCtx.GetConstraintsMap()).To(BeNil())
		})

		It("returns constraints map when constraints exist", func() {
			err := constraintCtx.MergeConstraints(map[string]any{
				"region": map[string]any{
					"const": "us-east-1",
				},
			}, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			constraints := constraintCtx.GetConstraintsMap()
			Expect(constraints).To(HaveKey("region"))
		})
	})
})
