package service

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AccumulatedAgentConstraints", func() {
	var constraintCtx *ConstraintContext

	BeforeEach(func() {
		constraintCtx = NewConstraintContext()
	})

	Context("MergeAgentConstraints", func() {
		It("merges allow lists by intersection", func() {
			first := &AccumulatedAgentConstraints{
				AllowList: []string{"agent-a", "agent-b", "agent-c"},
			}
			err := constraintCtx.MergeAgentConstraints(first, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			second := &AccumulatedAgentConstraints{
				AllowList: []string{"agent-b", "agent-c", "agent-d"},
			}
			err = constraintCtx.MergeAgentConstraints(second, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("agent-b", nil)
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("agent-a", nil)
			Expect(err).To(HaveOccurred())
		})

		It("enforces tightening-only across priorities", func() {
			first := &AccumulatedAgentConstraints{
				AllowList: []string{"agent-a"},
			}
			err := constraintCtx.MergeAgentConstraints(first, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			second := &AccumulatedAgentConstraints{
				AllowList: []string{"agent-a", "agent-b"},
			}
			err = constraintCtx.MergeAgentConstraints(second, "policy-2")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("agent-b", nil)
			Expect(err).To(HaveOccurred())
		})

		It("validates agent name against constraints", func() {
			constraints := &AccumulatedAgentConstraints{
				AllowList: []string{"allowed-agent"},
				Patterns:  []string{"^allowed-.*"},
			}
			err := constraintCtx.MergeAgentConstraints(constraints, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("allowed-agent", nil)
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("denied-agent", nil)
			Expect(err).To(HaveOccurred())
		})

		It("validates agent environment against environment_constraints", func() {
			constraints := &AccumulatedAgentConstraints{
				EnvironmentConstraints: []string{"production", "staging"},
			}
			err := constraintCtx.MergeAgentConstraints(constraints, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgentEnvironment("production")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgentEnvironment("development")
			Expect(err).To(HaveOccurred())
		})
	})

	// ValidateAgent must reject a selected agent outside availableAgents
	// even when no policy declared an allow-list at all.
	Context("ValidateAgent against availableAgents (fail-fast on agent selection)", func() {
		It("rejects a selected agent absent from availableAgents even with no other constraints set", func() {
			err := constraintCtx.ValidateAgent("rogue-agent", []string{"agent-a", "agent-b"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rogue-agent"))
		})

		It("accepts a selected agent present in availableAgents", func() {
			err := constraintCtx.ValidateAgent("agent-a", []string{"agent-a", "agent-b"})
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not enforce the available-agents check when availableAgents is empty", func() {
			err := constraintCtx.ValidateAgent("any-agent", nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("checks availableAgents membership before policy allow-list constraints", func() {
			constraints := &AccumulatedAgentConstraints{
				AllowList: []string{"rogue-agent"}, // would pass the allow-list alone
			}
			err := constraintCtx.MergeAgentConstraints(constraints, "policy-1")
			Expect(err).NotTo(HaveOccurred())

			err = constraintCtx.ValidateAgent("rogue-agent", []string{"agent-a", "agent-b"})
			Expect(err).To(HaveOccurred())
		})
	})
})
