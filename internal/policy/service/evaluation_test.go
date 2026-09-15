package service

import (
	"context"
	"errors"

	"github.com/dcm-project/control-plane/internal/policy/opa"
	"github.com/dcm-project/control-plane/internal/policy/store"
	"github.com/dcm-project/control-plane/internal/policy/store/model"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Test suite is registered in other test files - don't register again

// Mock implementations
type mockPolicyStore struct {
	policies []model.Policy
	err      error
}

func (m *mockPolicyStore) Create(_ context.Context, _ model.Policy) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) Get(_ context.Context, _ string) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) List(_ context.Context, _ *store.PolicyListOptions) (*store.PolicyListResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &store.PolicyListResult{
		Policies: m.policies,
	}, nil
}

func (m *mockPolicyStore) ListAll(_ context.Context) (model.PolicyList, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.policies, nil
}

func (m *mockPolicyStore) Update(_ context.Context, _ model.Policy) (*model.Policy, error) {
	return nil, errors.New("not implemented")
}

func (m *mockPolicyStore) Delete(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

type mockEngine struct {
	evaluations map[string]*opa.EvaluationResult
	err         error
}

func (m *mockEngine) Compile(_ context.Context, _ []opa.PolicyModule) error {
	return nil
}

func (m *mockEngine) ValidateRego(_ context.Context, _ string) error {
	return nil
}

func (m *mockEngine) EvaluatePolicy(_ context.Context, policyID string, _ map[string]any) (*opa.EvaluationResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if result, ok := m.evaluations[policyID]; ok {
		return result, nil
	}
	// Return undefined result by default
	return &opa.EvaluationResult{Defined: false}, nil
}

var _ = Describe("EvaluationService", func() {
	var (
		ctx         context.Context
		mockStore   *mockPolicyStore
		mockOPA     *mockEngine
		service     EvaluationService
		baseRequest *EvaluationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockStore = &mockPolicyStore{
			policies: []model.Policy{},
		}
		mockOPA = &mockEngine{
			evaluations: make(map[string]*opa.EvaluationResult),
		}
		service = NewEvaluationService(mockStore, mockOPA)

		baseRequest = &EvaluationRequest{
			ServiceInstance: map[string]any{},
			RequestLabels:   map[string]string{},
		}
	})

	Describe("EvaluateRequest", func() {
		Context("when no policies exist", func() {
			It("returns approved with unchanged spec", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{}))
				Expect(response.SelectedAgent).To(Equal(""))
			})
		})

		Context("when policies don't match label selectors", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:            "policy-1",
						Enabled:       true,
						PolicyType:    "GLOBAL",
						Priority:      100,
						LabelSelector: map[string]string{"env": "prod"},
					},
				}
			})

			It("returns approved with unchanged spec", func() {
				baseRequest.RequestLabels = map[string]string{"env": "dev"}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
			})
		})

		Context("when policy modifies the spec via patch", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
						"selected_agent": "aws-agent",
					},
				}
			})

			It("returns modified with updated spec and selected agent", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"region": "us-east-1",
				}))
				Expect(response.SelectedAgent).To(Equal("aws-agent"))
			})
		})

		Context("when patch merges with existing spec", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}
			})

			It("preserves existing spec fields not in patch", func() {
				baseRequest.ServiceInstance = map[string]any{
					"instance_type": "t3.medium",
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"instance_type": "t3.medium",
					"region":        "us-east-1",
				}))
			})
		})

		Context("when policy rejects the request", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected":         true,
						"rejection_reason": "Security policy violation",
					},
				}
			})

			It("returns policy rejected error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypeRejected))
				Expect(serviceErr.Message).To(ContainSubstring("policy-1"))
				Expect(serviceErr.Detail).To(Equal("Security policy violation"))
			})
		})

		Context("when lower-priority policy violates constraint", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets region with a const constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
						"constraints": map[string]any{
							"region": map[string]any{
								"const": "us-east-1",
							},
						},
					},
				}

				// Second policy tries to change region, violating the constraint
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-west-2",
						},
					},
				}
			})

			It("returns policy conflict error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
			})
		})

		Context("when lower-priority policy tries to loosen constraint", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets minimum constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(4),
								"maximum": float64(8),
							},
						},
					},
				}

				// Second policy tries to lower the minimum — loosening
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(1),
							},
						},
					},
				}
			})

			It("returns constraint conflict error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
				Expect(serviceErr.Detail).To(ContainSubstring("loosen"))
			})
		})

		Context("when policies evaluate sequentially without conflicts", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "USER",
						Priority:   100,
					},
				}

				// First policy adds region
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}

				// Second policy adds instance_type (no conflict — no constraint on region)
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"instance_type": "t3.medium",
						},
					},
				}
			})

			It("applies both policies successfully", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance).To(Equal(map[string]any{
					"region":        "us-east-1",
					"instance_type": "t3.medium",
				}))
			})
		})

		Context("when policy sets value with range constraint allowing further changes", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "USER",
						Priority:   100,
					},
				}

				// First policy sets cpu_count=2 with range constraint 1-4
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"cpu_count": float64(2),
						},
						"constraints": map[string]any{
							"cpu_count": map[string]any{
								"minimum": float64(1),
								"maximum": float64(4),
							},
						},
					},
				}

				// Second policy changes cpu_count to 4 — within constraint range
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"cpu_count": float64(4),
						},
					},
				}
			})

			It("allows the change within the constraint range", func() {
				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
				Expect(response.EvaluatedServiceInstance["cpu_count"]).To(Equal(float64(4)))
			})
		})

		Context("when policy evaluation fails", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
				}

				mockOPA.err = errors.New("OPA unavailable")
			})

			It("returns internal error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypeInternal))
			})
		})

		Context("when label selector matches", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,

						LabelSelector: map[string]string{"env": "prod", "team": "backend"},
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"patch": map[string]any{
							"region": "us-east-1",
						},
					},
				}
			})

			It("applies policy when all labels match", func() {
				baseRequest.RequestLabels = map[string]string{
					"env":  "prod",
					"team": "backend",
					"app":  "web", // Extra label is OK
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusModified))
			})

			It("skips policy when labels don't match", func() {
				baseRequest.RequestLabels = map[string]string{
					"env": "prod",
					// Missing "team" label
				}

				response, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).NotTo(HaveOccurred())
				Expect(response.Status).To(Equal(EvaluationStatusApproved))
			})
		})

		Context("when OPA input includes accumulated constraints", func() {
			var capturedInput map[string]any

			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				// First policy sets constraint
				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"constraints": map[string]any{
							"region": map[string]any{
								"enum": []any{"us-east-1", "us-west-2"},
							},
						},
					},
				}

				// Capture input for second policy
				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: false,
				}
			})

			It("passes constraints in OPA input for subsequent policies", func() {
				// Override the mock to capture the input
				originalEval := mockOPA.evaluations
				mockOPA.evaluations = nil

				evalCount := 0
				customOPA := &mockEngineWithCapture{
					evaluations: originalEval,
					captureFunc: func(input map[string]any) {
						evalCount++
						if evalCount == 2 {
							capturedInput = input
						}
					},
				}

				service = NewEvaluationService(mockStore, customOPA)
				_, _ = service.EvaluateRequest(ctx, baseRequest)

				Expect(capturedInput).To(HaveKey("constraints"))
			})
		})

		Context("when agent constraints are enforced via allow_list", func() {
			BeforeEach(func() {
				mockStore.policies = []model.Policy{
					{
						ID:         "policy-1",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   100,
					},
					{
						ID:         "policy-2",
						Enabled:    true,
						PolicyType: "GLOBAL",
						Priority:   200,
					},
				}

				mockOPA.evaluations["policy-1"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected": false,
						"agent_constraints": map[string]any{
							"allow_list": []any{"aws-agent", "gcp-agent"},
						},
					},
				}

				mockOPA.evaluations["policy-2"] = &opa.EvaluationResult{
					Defined: true,
					Result: map[string]any{
						"rejected":       false,
						"selected_agent": "azure-agent",
					},
				}
			})

			It("returns agent constraint error", func() {
				_, err := service.EvaluateRequest(ctx, baseRequest)

				Expect(err).To(HaveOccurred())
				serviceErr, ok := err.(*ServiceError)
				Expect(ok).To(BeTrue())
				Expect(serviceErr.Type).To(Equal(ErrorTypePolicyConflict))
				Expect(serviceErr.Message).To(ContainSubstring("policy-2"))
			})
		})
	})
})

var _ = Describe("EvaluateRequest with agents", func() {
	var (
		ctx         context.Context
		mockStore   *mockPolicyStore
		mockOPA     *mockEngine
		svc         EvaluationService
		baseRequest *EvaluationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockStore = &mockPolicyStore{policies: []model.Policy{}}
		mockOPA = &mockEngine{evaluations: make(map[string]*opa.EvaluationResult)}
		svc = NewEvaluationService(mockStore, mockOPA)
		baseRequest = &EvaluationRequest{
			ServiceInstance: map[string]any{},
			RequestLabels:   map[string]string{},
		}
	})

	It("passes available_agents to OPA input", func() {
		baseRequest.AvailableAgents = agentInfosForVM("agent-a", "agent-b")
		baseRequest.ServiceInstance["service_type"] = "vm"

		var capturedInput map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: make(map[string]*opa.EvaluationResult),
			captureFunc: func(input map[string]any) { capturedInput = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		mockStore.policies = []model.Policy{
			{ID: "p1", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		captureOPA.evaluations["p1"] = &opa.EvaluationResult{Defined: true, Result: map[string]any{}}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(capturedInput).To(HaveKey("available_agents"))
	})

	It("pre-filters exclude_agents before evaluation", func() {
		baseRequest.AvailableAgents = agentInfosForVM("agent-a", "agent-b", "agent-c")
		baseRequest.ExcludeAgents = []string{"agent-b"}
		baseRequest.ServiceInstance["service_type"] = "vm"

		var capturedInput map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: make(map[string]*opa.EvaluationResult),
			captureFunc: func(input map[string]any) { capturedInput = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		mockStore.policies = []model.Policy{
			{ID: "p1", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		captureOPA.evaluations["p1"] = &opa.EvaluationResult{Defined: true, Result: map[string]any{}}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())

		agents, ok := capturedInput["available_agents"].([]map[string]any)
		Expect(ok).To(BeTrue())
		names := make([]string, 0, len(agents))
		for _, a := range agents {
			names = append(names, a["name"].(string))
		}
		Expect(names).NotTo(ContainElement("agent-b"))
	})

	It("includes exclude_agents in the OPA input for policy transparency", func() {
		baseRequest.AvailableAgents = agentInfosForVM("agent-a", "agent-b", "agent-c")
		baseRequest.ExcludeAgents = []string{"agent-b"}
		baseRequest.ServiceInstance["service_type"] = "vm"

		var capturedInput map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: make(map[string]*opa.EvaluationResult),
			captureFunc: func(input map[string]any) { capturedInput = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		mockStore.policies = []model.Policy{
			{ID: "p1", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		captureOPA.evaluations["p1"] = &opa.EvaluationResult{Defined: true, Result: map[string]any{}}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())

		excluded, ok := capturedInput["exclude_agents"].([]string)
		Expect(ok).To(BeTrue())
		Expect(excluded).To(ConsistOf("agent-b"))
	})

	It("omits exclude_agents from the OPA input when nothing was excluded", func() {
		baseRequest.AvailableAgents = agentInfosForVM("agent-a")
		baseRequest.ServiceInstance["service_type"] = "vm"

		var capturedInput map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: make(map[string]*opa.EvaluationResult),
			captureFunc: func(input map[string]any) { capturedInput = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		mockStore.policies = []model.Policy{
			{ID: "p1", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		captureOPA.evaluations["p1"] = &opa.EvaluationResult{Defined: true, Result: map[string]any{}}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(capturedInput).NotTo(HaveKey("exclude_agents"))
	})

	It("returns selected_agent in response", func() {
		mockStore.policies = []model.Policy{
			{ID: "agent-policy", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		mockOPA.evaluations["agent-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result: map[string]any{
				"selected_agent": "my-agent",
			},
		}

		resp, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.SelectedAgent).To(Equal("my-agent"))
	})

	It("validates selected_agent against agent_constraints", func() {
		mockStore.policies = []model.Policy{
			{ID: "constraint-policy", PolicyType: "constraint", Priority: 1, Enabled: true},
			{ID: "routing-policy", PolicyType: "routing", Priority: 2, Enabled: true},
		}
		mockOPA.evaluations["constraint-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result: map[string]any{
				"agent_constraints": map[string]any{
					"allow_list": []any{"allowed-agent"},
				},
			},
		}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result: map[string]any{
				"selected_agent": "denied-agent",
			},
		}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).To(HaveOccurred())
	})

	// A misconfigured policy with no agent_constraints must still be
	// rejected if it selects an agent outside available_agents.
	It("rejects a selected_agent absent from AvailableAgents even when no agent_constraints policy ran", func() {
		baseRequest.AvailableAgents = agentInfos("agent-a", "agent-b")
		mockStore.policies = []model.Policy{
			{ID: "routing-policy", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result: map[string]any{
				"selected_agent": "rogue-agent",
			},
		}

		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Detail).To(ContainSubstring("rogue-agent"))
	})

	It("accepts a selected_agent present in AvailableAgents when no agent_constraints policy ran", func() {
		baseRequest.AvailableAgents = agentInfos("agent-a", "agent-b")
		mockStore.policies = []model.Policy{
			{ID: "routing-policy", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result: map[string]any{
				"selected_agent": "agent-b",
			},
		}

		resp, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).NotTo(HaveOccurred())
		Expect(resp.SelectedAgent).To(Equal("agent-b"))
	})
})

var _ = Describe("EvaluateRequest service-type capability filtering", func() {
	var (
		ctx         context.Context
		mockStore   *mockPolicyStore
		mockOPA     *mockEngine
		svc         EvaluationService
		baseRequest *EvaluationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockStore = &mockPolicyStore{policies: []model.Policy{
			{ID: "routing-policy", PolicyType: "routing", Priority: 1, Enabled: true},
		}}
		mockOPA = &mockEngine{evaluations: map[string]*opa.EvaluationResult{
			"routing-policy": {Defined: true, Result: map[string]any{"selected_agent": "db-agent"}},
		}}
		svc = NewEvaluationService(mockStore, mockOPA)
		baseRequest = &EvaluationRequest{
			ServiceInstance: map[string]any{"service_type": "database"},
			RequestLabels:   map[string]string{},
			AvailableAgents: []AgentInfo{
				{Name: "vm-agent", ServiceTypes: []string{"vm"}},
				{Name: "db-agent", ServiceTypes: []string{"database"}},
			},
		}
	})

	It("excludes agents that don't support the requested service type from evaluation", func() {
		captured := map[string]any{}
		captureOPA := &mockEngineWithCapture{
			evaluations: mockOPA.evaluations,
			captureFunc: func(input map[string]any) { captured = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		resp, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.SelectedAgent).To(Equal("db-agent"))

		agents, ok := captured["available_agents"].([]map[string]any)
		Expect(ok).To(BeTrue())
		names := make([]string, 0, len(agents))
		for _, a := range agents {
			names = append(names, a["name"].(string))
		}
		Expect(names).To(ConsistOf("db-agent"))
	})

	It("exposes each agent's service_types in the OPA input", func() {
		var captured map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: mockOPA.evaluations,
			captureFunc: func(input map[string]any) { captured = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())

		agents, ok := captured["available_agents"].([]map[string]any)
		Expect(ok).To(BeTrue())
		// Only db-agent survives the capability filter (vm-agent is
		// dropped), so assert unconditionally rather than inside a
		// name-matching loop that would pass vacuously if filtering
		// silently returned zero agents.
		Expect(agents).To(HaveLen(1))
		Expect(agents[0]["name"]).To(Equal("db-agent"))
		Expect(agents[0]["service_types"]).To(Equal([]string{"database"}))
	})

	It("exposes an empty array (not null) for an agent with nil ServiceTypes when no capability filter runs", func() {
		baseRequest.ServiceInstance = map[string]any{}
		baseRequest.AvailableAgents = []AgentInfo{{Name: "no-types-agent"}}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result:  map[string]any{"selected_agent": "no-types-agent"},
		}

		var captured map[string]any
		captureOPA := &mockEngineWithCapture{
			evaluations: mockOPA.evaluations,
			captureFunc: func(input map[string]any) { captured = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())

		agents, ok := captured["available_agents"].([]map[string]any)
		Expect(ok).To(BeTrue())
		Expect(agents).To(HaveLen(1))
		Expect(agents[0]["service_types"]).To(Equal([]string{}))
	})

	It("matches when an agent supports the requested type among several", func() {
		baseRequest.AvailableAgents = []AgentInfo{
			{Name: "multi-agent", ServiceTypes: []string{"vm", "database", "storage"}},
		}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result:  map[string]any{"selected_agent": "multi-agent"},
		}

		resp, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.SelectedAgent).To(Equal("multi-agent"))
	})

	It("rejects with a clear error when excluding the only capable agent leaves none, even though other (incapable) agents remain", func() {
		baseRequest.ExcludeAgents = []string{"db-agent"}

		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Type).To(Equal(ErrorTypeRejected))
		Expect(serviceErr.Detail).To(ContainSubstring("database"))
		Expect(serviceErr.Detail).To(ContainSubstring("1 agent"))
	})

	It("rejects with a validation error when service_type is present but not a string", func() {
		baseRequest.ServiceInstance["service_type"] = 42

		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Type).To(Equal(ErrorTypeInvalidArgument))
	})

	It("rejects with a validation error when service_type is an empty or whitespace-only string", func() {
		baseRequest.ServiceInstance["service_type"] = "   "

		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Type).To(Equal(ErrorTypeInvalidArgument))
	})

	It("rejects with a clear error when no available agent supports the requested service type", func() {
		baseRequest.ServiceInstance["service_type"] = "storage"

		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Type).To(Equal(ErrorTypeRejected))
		Expect(serviceErr.Message).To(ContainSubstring("storage"))
	})

	It("does not filter when the service instance has no service_type", func() {
		baseRequest.ServiceInstance = map[string]any{}

		captured := map[string]any{}
		captureOPA := &mockEngineWithCapture{
			evaluations: mockOPA.evaluations,
			captureFunc: func(input map[string]any) { captured = input },
		}
		svc = NewEvaluationService(mockStore, captureOPA)

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())

		agents, ok := captured["available_agents"].([]map[string]any)
		Expect(ok).To(BeTrue())
		Expect(agents).To(HaveLen(2))
	})

	It("does not filter agents that were already excluded via exclude_agents", func() {
		// Both remaining candidates support "database", so exclusion alone
		// (not capability) determines what's left.
		baseRequest.AvailableAgents = []AgentInfo{
			{Name: "db-agent-1", ServiceTypes: []string{"database"}},
			{Name: "db-agent-2", ServiceTypes: []string{"database"}},
		}
		baseRequest.ExcludeAgents = []string{"db-agent-1"}
		mockOPA.evaluations["routing-policy"] = &opa.EvaluationResult{
			Defined: true,
			Result:  map[string]any{"selected_agent": "db-agent-2"},
		}

		resp, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.SelectedAgent).To(Equal("db-agent-2"))
	})
})

var _ = Describe("EvaluateRequest all-agents-excluded guard", func() {
	var (
		ctx         context.Context
		mockStore   *mockPolicyStore
		mockOPA     *mockEngine
		svc         EvaluationService
		baseRequest *EvaluationRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockStore = &mockPolicyStore{policies: []model.Policy{}}
		mockOPA = &mockEngine{evaluations: make(map[string]*opa.EvaluationResult)}
		svc = NewEvaluationService(mockStore, mockOPA)
		baseRequest = &EvaluationRequest{
			ServiceInstance: map[string]any{},
			RequestLabels:   map[string]string{},
			AvailableAgents: agentInfos("only-agent"),
			ExcludeAgents:   []string{"only-agent"},
		}
	})

	It("rejects when exclusion removes every available agent, instead of falling through to Rego with none", func() {
		_, err := svc.EvaluateRequest(ctx, baseRequest)

		Expect(err).To(HaveOccurred())
		serviceErr, ok := err.(*ServiceError)
		Expect(ok).To(BeTrue())
		Expect(serviceErr.Type).To(Equal(ErrorTypeRejected))
		Expect(serviceErr.Detail).To(ContainSubstring("1"))
	})

	It("does not reject when there were no available agents to begin with (no agent client configured)", func() {
		baseRequest.AvailableAgents = nil
		baseRequest.ExcludeAgents = nil
		mockStore.policies = []model.Policy{
			{ID: "p1", PolicyType: "routing", Priority: 1, Enabled: true},
		}
		mockOPA.evaluations["p1"] = &opa.EvaluationResult{Defined: true, Result: map[string]any{}}

		_, err := svc.EvaluateRequest(ctx, baseRequest)
		Expect(err).NotTo(HaveOccurred())
	})
})

// agentInfos builds []AgentInfo from bare names (Environment left blank)
// for tests that only care about name-based membership/filtering.
func agentInfos(names ...string) []AgentInfo {
	infos := make([]AgentInfo, len(names))
	for i, n := range names {
		infos[i] = AgentInfo{Name: n}
	}
	return infos
}

// agentInfosForVM builds []AgentInfo from bare names, each declaring "vm"
// as a supported service type, for tests that set
// ServiceInstance["service_type"] = "vm" but only care about name-based
// membership/filtering (not capability filtering itself).
func agentInfosForVM(names ...string) []AgentInfo {
	infos := make([]AgentInfo, len(names))
	for i, n := range names {
		infos[i] = AgentInfo{Name: n, ServiceTypes: []string{"vm"}}
	}
	return infos
}

// mockEngineWithCapture wraps mockEngine and captures inputs
type mockEngineWithCapture struct {
	evaluations map[string]*opa.EvaluationResult
	captureFunc func(input map[string]any)
}

func (m *mockEngineWithCapture) Compile(_ context.Context, _ []opa.PolicyModule) error {
	return nil
}

func (m *mockEngineWithCapture) ValidateRego(_ context.Context, _ string) error {
	return nil
}

func (m *mockEngineWithCapture) EvaluatePolicy(_ context.Context, policyID string, input map[string]any) (*opa.EvaluationResult, error) {
	if m.captureFunc != nil {
		m.captureFunc(input)
	}
	if result, ok := m.evaluations[policyID]; ok {
		return result, nil
	}
	return &opa.EvaluationResult{Defined: false}, nil
}
