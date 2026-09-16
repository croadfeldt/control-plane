package v1alpha1_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1alpha1API "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/api/server"
	v1alpha1 "github.com/dcm-project/control-plane/internal/catalog/handlers/v1alpha1"
	"github.com/dcm-project/control-plane/internal/catalog/service"
)

// Mock CatalogItemInstanceService for testing
type mockCatalogItemInstanceService struct {
	listFunc      func(ctx context.Context, opts service.CatalogItemInstanceListOptions) (*service.CatalogItemInstanceListResult, error)
	createFunc    func(ctx context.Context, req *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error)
	getFunc       func(ctx context.Context, id string) (*v1alpha1API.CatalogItemInstance, error)
	deleteFunc    func(ctx context.Context, id string) error
	rehydrateFunc func(ctx context.Context, id string) (*v1alpha1API.CatalogItemInstance, error)
}

func (m *mockCatalogItemInstanceService) List(ctx context.Context, opts service.CatalogItemInstanceListOptions) (*service.CatalogItemInstanceListResult, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return &service.CatalogItemInstanceListResult{}, nil
}

func (m *mockCatalogItemInstanceService) Create(ctx context.Context, req *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &v1alpha1API.CatalogItemInstance{}, nil
}

func (m *mockCatalogItemInstanceService) Get(ctx context.Context, id string) (*v1alpha1API.CatalogItemInstance, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &v1alpha1API.CatalogItemInstance{}, nil
}

func (m *mockCatalogItemInstanceService) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockCatalogItemInstanceService) Rehydrate(ctx context.Context, id string) (*v1alpha1API.CatalogItemInstance, error) {
	if m.rehydrateFunc != nil {
		return m.rehydrateFunc(ctx, id)
	}
	return &v1alpha1API.CatalogItemInstance{}, nil
}

// Mock Service with CatalogItemInstance
type mockCatalogItemInstanceServiceWrapper struct {
	catalogItemInstanceService service.CatalogItemInstanceService
}

func (m *mockCatalogItemInstanceServiceWrapper) ServiceType() service.ServiceTypeService {
	return nil
}

func (m *mockCatalogItemInstanceServiceWrapper) CatalogItem() service.CatalogItemService {
	return nil
}

func (m *mockCatalogItemInstanceServiceWrapper) CatalogItemInstance() service.CatalogItemInstanceService {
	return m.catalogItemInstanceService
}

func (m *mockCatalogItemInstanceServiceWrapper) Seed(_ context.Context) error {
	return nil
}

var _ = Describe("CatalogItemInstance Handler", func() {
	var (
		ctx             context.Context
		handler         *v1alpha1.Handler
		mockCIIService  *mockCatalogItemInstanceService
		mockSvc         service.Service
		testTime        time.Time
		testID          string
		testRunID       = "test-run-id"
		testPath        string
		testApiVersion  = "v1alpha1"
		testCatalogItem = "small-vm"
	)

	BeforeEach(func() {
		ctx = context.Background()
		testTime = time.Now()
		testID = "test-instance-id"
		testRunID = "test-run-id"
		testPath = "catalog-item-instances/" + testID
		mockCIIService = &mockCatalogItemInstanceService{}
		mockSvc = &mockCatalogItemInstanceServiceWrapper{catalogItemInstanceService: mockCIIService}
		handler = v1alpha1.NewHandler(mockSvc, slog.Default())
	})

	Describe("CreateCatalogItemInstance", func() {
		Context("with valid request", func() {
			It("should create an instance and return 201", func() {
				mockCIIService.createFunc = func(_ context.Context, req *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error) {
					Expect(req.DisplayName).To(Equal("My Instance"))
					Expect(req.ApiVersion).To(Equal("v1alpha1"))
					Expect(req.Spec.CatalogItemId).To(Equal(testCatalogItem))
					return &v1alpha1API.CatalogItemInstance{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  testApiVersion,
						DisplayName: "My Instance",
						RunId:       &testRunID,
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{{Path: "spec.vcpu.count", Value: 4}},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.CreateCatalogItemInstanceRequestObject{
					Body: &v1alpha1API.CatalogItemInstance{
						ApiVersion:  testApiVersion,
						DisplayName: "My Instance",
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{{Path: "spec.vcpu.count", Value: 4}},
						},
					},
				}

				response, err := handler.CreateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance201JSONResponse{}))

				created := response.(server.CreateCatalogItemInstance201JSONResponse)
				Expect(*created.Uid).To(Equal(testID))
				Expect(created.DisplayName).To(Equal("My Instance"))
			})

			It("should handle optional ID query param", func() {
				userID := "my-custom-id"
				mockCIIService.createFunc = func(_ context.Context, req *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error) {
					Expect(req.ID).ToNot(BeNil())
					Expect(*req.ID).To(Equal(userID))
					path := "catalog-item-instances/" + userID
					return &v1alpha1API.CatalogItemInstance{
						Uid:         &userID,
						Path:        &path,
						ApiVersion:  testApiVersion,
						DisplayName: "Custom ID",
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.CreateCatalogItemInstanceRequestObject{
					Params: v1alpha1API.CreateCatalogItemInstanceParams{Id: &userID},
					Body: &v1alpha1API.CatalogItemInstance{
						ApiVersion:  testApiVersion,
						DisplayName: "Custom ID",
						RunId:       &testRunID,
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
					},
				}

				response, err := handler.CreateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				created := response.(server.CreateCatalogItemInstance201JSONResponse)
				Expect(*created.Uid).To(Equal(userID))
			})
		})

		Context("with validation errors", func() {
			It("should return 400 when api_version is not v1alpha1", func() {
				request := server.CreateCatalogItemInstanceRequestObject{
					Body: &v1alpha1API.CatalogItemInstance{
						ApiVersion:  "v2beta1",
						DisplayName: "My Instance",
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
					},
				}

				response, err := handler.CreateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance400JSONResponse{}))

				badRequest := response.(server.CreateCatalogItemInstance400JSONResponse)
				Expect(badRequest.Status).To(Equal(int32(400)))
				Expect(badRequest.Type).To(Equal(v1alpha1API.INVALIDARGUMENT))
				Expect(*badRequest.Detail).To(ContainSubstring("api_version"))
			})
		})

		Context("with duplicate ID", func() {
			It("should return 409 conflict", func() {
				mockCIIService.createFunc = func(_ context.Context, _ *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error) {
					return nil, service.ErrCatalogItemInstanceIDTaken
				}

				request := server.CreateCatalogItemInstanceRequestObject{
					Body: &v1alpha1API.CatalogItemInstance{
						ApiVersion:  testApiVersion,
						DisplayName: "Duplicate",
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
					},
				}

				response, err := handler.CreateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance409JSONResponse{}))

				conflict := response.(server.CreateCatalogItemInstance409JSONResponse)
				Expect(conflict.Status).To(Equal(int32(409)))
				Expect(conflict.Type).To(Equal(v1alpha1API.ALREADYEXISTS))
			})
		})

		DescribeTable("maps service errors to HTTP responses",
			func(serviceErr error, expectedStatus int32, expectedType v1alpha1API.ErrorType, expectedTitle string) {
				mockCIIService.createFunc = func(_ context.Context, _ *service.CreateCatalogItemInstanceRequest) (*v1alpha1API.CatalogItemInstance, error) {
					return nil, serviceErr
				}

				request := server.CreateCatalogItemInstanceRequestObject{
					Body: &v1alpha1API.CatalogItemInstance{
						ApiVersion:  testApiVersion,
						DisplayName: "Test",
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
					},
				}

				response, err := handler.CreateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				switch expectedStatus {
				case 400:
					Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance400JSONResponse{}))
					resp := response.(server.CreateCatalogItemInstance400JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 406:
					Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance406JSONResponse{}))
					resp := response.(server.CreateCatalogItemInstance406JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 422:
					Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance422JSONResponse{}))
					resp := response.(server.CreateCatalogItemInstance422JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 424:
					Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance424JSONResponse{}))
					resp := response.(server.CreateCatalogItemInstance424JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 500:
					Expect(response).To(BeAssignableToTypeOf(server.CreateCatalogItemInstance500JSONResponse{}))
					resp := response.(server.CreateCatalogItemInstance500JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				default:
					Fail(fmt.Sprintf("unexpected status in test case: %d", expectedStatus))
				}
			},
			Entry("catalog item not found", service.ErrCatalogItemNotFoundForInstance, int32(400), v1alpha1API.INVALIDARGUMENT, "Bad Request"),
			Entry("user value path not found", service.ErrUserValuePathNotFound, int32(400), v1alpha1API.INVALIDARGUMENT, "Bad Request"),
			Entry("user value not editable", service.ErrUserValueNotEditable, int32(400), v1alpha1API.INVALIDARGUMENT, "Bad Request"),
			Entry("user value validation failed", service.ErrUserValueValidationFailed, int32(400), v1alpha1API.INVALIDARGUMENT, "Bad Request"),
			Entry("placement manager policy rejected", service.ErrPlacementManagerPolicyRejected, int32(406), v1alpha1API.FAILEDPRECONDITION, "Policy Rejected"),
			Entry("placement manager provider error", service.ErrPlacementManagerProviderError, int32(422), v1alpha1API.FAILEDPRECONDITION, "Provider Error"),
			Entry("placement manager policy dependency", service.ErrPlacementManagerPolicyDependency, int32(424), v1alpha1API.FAILEDPRECONDITION, "Policy Dependency"),
			Entry("placement manager create failed", service.ErrPlacementManagerCreateFailed, int32(500), v1alpha1API.INTERNAL, "Placement Manager Error"),
			Entry("generic service error", errors.New("database error"), int32(500), v1alpha1API.INTERNAL, "Internal Server Error"),
		)
	})

	Describe("ListCatalogItemInstances", func() {
		Context("with valid request", func() {
			It("should list instances and return 200", func() {
				mockCIIService.listFunc = func(_ context.Context, _ service.CatalogItemInstanceListOptions) (*service.CatalogItemInstanceListResult, error) {
					return &service.CatalogItemInstanceListResult{
						CatalogItemInstances: []v1alpha1API.CatalogItemInstance{
							{
								Uid:         &testID,
								Path:        &testPath,
								ApiVersion:  testApiVersion,
								DisplayName: "Instance 1",
								RunId:       &testRunID,
								Spec: v1alpha1API.CatalogItemInstanceSpec{
									CatalogItemId: testCatalogItem,
									UserValues:    []v1alpha1API.UserValue{},
								},
							},
						},
					}, nil
				}

				request := server.ListCatalogItemInstancesRequestObject{}

				response, err := handler.ListCatalogItemInstances(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListCatalogItemInstances200JSONResponse{}))

				list := response.(server.ListCatalogItemInstances200JSONResponse)
				Expect(list.Results).To(HaveLen(1))
			})

			It("should pass pagination and filter params correctly", func() {
				pageToken := "token123"
				pageSize := int32(10)
				catalogItemFilter := "small-vm"
				nextToken := "next-token"
				mockCIIService.listFunc = func(_ context.Context, opts service.CatalogItemInstanceListOptions) (*service.CatalogItemInstanceListResult, error) {
					Expect(opts.PageToken).To(Equal(&pageToken))
					Expect(opts.MaxPageSize).To(Equal(&pageSize))
					Expect(opts.CatalogItemId).To(Equal(&catalogItemFilter))
					return &service.CatalogItemInstanceListResult{
						CatalogItemInstances: []v1alpha1API.CatalogItemInstance{},
						NextPageToken:        &nextToken,
					}, nil
				}

				request := server.ListCatalogItemInstancesRequestObject{
					Params: v1alpha1API.ListCatalogItemInstancesParams{
						PageToken:     &pageToken,
						MaxPageSize:   &pageSize,
						CatalogItemId: &catalogItemFilter,
					},
				}

				response, err := handler.ListCatalogItemInstances(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				list := response.(server.ListCatalogItemInstances200JSONResponse)
				Expect(list.NextPageToken).To(Equal(nextToken))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIIService.listFunc = func(_ context.Context, _ service.CatalogItemInstanceListOptions) (*service.CatalogItemInstanceListResult, error) {
					return nil, errors.New("database error")
				}

				request := server.ListCatalogItemInstancesRequestObject{}

				response, err := handler.ListCatalogItemInstances(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.ListCatalogItemInstances500JSONResponse{}))

				serverError := response.(server.ListCatalogItemInstances500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
			})
		})
	})

	Describe("GetCatalogItemInstance", func() {
		Context("with valid request", func() {
			It("should get an instance and return 200", func() {
				mockCIIService.getFunc = func(_ context.Context, id string) (*v1alpha1API.CatalogItemInstance, error) {
					Expect(id).To(Equal(testID))
					return &v1alpha1API.CatalogItemInstance{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  testApiVersion,
						DisplayName: "Test Instance",
						RunId:       &testRunID,
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.GetCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.GetCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItemInstance200JSONResponse{}))

				item := response.(server.GetCatalogItemInstance200JSONResponse)
				Expect(*item.Uid).To(Equal(testID))
			})
		})

		Context("with not found error", func() {
			It("should return 404 not found", func() {
				mockCIIService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.CatalogItemInstance, error) {
					return nil, service.ErrCatalogItemInstanceNotFound
				}

				request := server.GetCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: "nonexistent",
				}

				response, err := handler.GetCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItemInstance404JSONResponse{}))

				notFound := response.(server.GetCatalogItemInstance404JSONResponse)
				Expect(notFound.Status).To(Equal(int32(404)))
				Expect(notFound.Type).To(Equal(v1alpha1API.NOTFOUND))
			})
		})

		Context("with service error", func() {
			It("should return 500 internal server error", func() {
				mockCIIService.getFunc = func(_ context.Context, _ string) (*v1alpha1API.CatalogItemInstance, error) {
					return nil, errors.New("database error")
				}

				request := server.GetCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.GetCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.GetCatalogItemInstance500JSONResponse{}))

				serverError := response.(server.GetCatalogItemInstance500JSONResponse)
				Expect(serverError.Status).To(Equal(int32(500)))
				Expect(serverError.Type).To(Equal(v1alpha1API.INTERNAL))
			})
		})
	})

	Describe("RehydrateCatalogItemInstance", func() {
		Context("with valid request", func() {
			It("should rehydrate instance and return 200", func() {
				newRunID := "new-run-id"
				mockCIIService.rehydrateFunc = func(_ context.Context, id string) (*v1alpha1API.CatalogItemInstance, error) {
					Expect(id).To(Equal(testID))
					return &v1alpha1API.CatalogItemInstance{
						Uid:         &testID,
						Path:        &testPath,
						ApiVersion:  testApiVersion,
						DisplayName: "Rehydrated Instance",
						RunId:       &newRunID,
						Spec: v1alpha1API.CatalogItemInstanceSpec{
							CatalogItemId: testCatalogItem,
							UserValues:    []v1alpha1API.UserValue{},
						},
						CreateTime: &testTime,
						UpdateTime: &testTime,
					}, nil
				}

				request := server.RehydrateCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.RehydrateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance200JSONResponse{}))

				rehydrated := response.(server.RehydrateCatalogItemInstance200JSONResponse)
				Expect(*rehydrated.Uid).To(Equal(testID))
				Expect(*rehydrated.RunId).To(Equal(newRunID))
			})
		})

		DescribeTable("maps service errors to HTTP responses",
			func(serviceErr error, expectedStatus int32, expectedType v1alpha1API.ErrorType, expectedTitle string) {
				mockCIIService.rehydrateFunc = func(_ context.Context, _ string) (*v1alpha1API.CatalogItemInstance, error) {
					return nil, serviceErr
				}

				request := server.RehydrateCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.RehydrateCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				switch expectedStatus {
				case 404:
					Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance404JSONResponse{}))
					resp := response.(server.RehydrateCatalogItemInstance404JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 406:
					Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance406JSONResponse{}))
					resp := response.(server.RehydrateCatalogItemInstance406JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 422:
					Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance422JSONResponse{}))
					resp := response.(server.RehydrateCatalogItemInstance422JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 424:
					Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance424JSONResponse{}))
					resp := response.(server.RehydrateCatalogItemInstance424JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 500:
					Expect(response).To(BeAssignableToTypeOf(server.RehydrateCatalogItemInstance500JSONResponse{}))
					resp := response.(server.RehydrateCatalogItemInstance500JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				default:
					Fail(fmt.Sprintf("unexpected status in test case: %d", expectedStatus))
				}
			},
			Entry("not found", service.ErrCatalogItemInstanceNotFound, int32(404), v1alpha1API.NOTFOUND, "Not Found"),
			Entry("placement manager policy rejected", service.ErrPlacementManagerPolicyRejected, int32(406), v1alpha1API.FAILEDPRECONDITION, "Policy Rejected"),
			Entry("placement manager provider error", service.ErrPlacementManagerProviderError, int32(422), v1alpha1API.FAILEDPRECONDITION, "Provider Error"),
			Entry("placement manager policy dependency", service.ErrPlacementManagerPolicyDependency, int32(424), v1alpha1API.FAILEDPRECONDITION, "Policy Dependency"),
			Entry("placement manager rehydrate failed", service.ErrPlacementManagerRehydrateFailed, int32(500), v1alpha1API.INTERNAL, "Placement Manager Error"),
			Entry("generic service error", errors.New("database error"), int32(500), v1alpha1API.INTERNAL, "Internal Server Error"),
		)
	})

	Describe("DeleteCatalogItemInstance", func() {
		Context("with valid request", func() {
			It("should delete instance and return 204", func() {
				mockCIIService.deleteFunc = func(_ context.Context, id string) error {
					Expect(id).To(Equal(testID))
					return nil
				}

				request := server.DeleteCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.DeleteCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItemInstance204Response{}))
			})
		})

		DescribeTable("maps service errors to HTTP responses",
			func(serviceErr error, expectedStatus int32, expectedType v1alpha1API.ErrorType, expectedTitle string) {
				mockCIIService.deleteFunc = func(_ context.Context, _ string) error {
					return serviceErr
				}

				request := server.DeleteCatalogItemInstanceRequestObject{
					CatalogItemInstanceId: testID,
				}

				response, err := handler.DeleteCatalogItemInstance(ctx, request)
				Expect(err).ToNot(HaveOccurred())

				switch expectedStatus {
				case 404:
					Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItemInstance404JSONResponse{}))
					resp := response.(server.DeleteCatalogItemInstance404JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				case 500:
					Expect(response).To(BeAssignableToTypeOf(server.DeleteCatalogItemInstance500JSONResponse{}))
					resp := response.(server.DeleteCatalogItemInstance500JSONResponse)
					Expect(resp.Status).To(Equal(expectedStatus))
					Expect(resp.Type).To(Equal(expectedType))
					Expect(resp.Title).To(Equal(expectedTitle))
				default:
					Fail(fmt.Sprintf("unexpected status in test case: %d", expectedStatus))
				}
			},
			Entry("not found", service.ErrCatalogItemInstanceNotFound, int32(404), v1alpha1API.NOTFOUND, "Not Found"),
			Entry("placement manager delete failed", service.ErrPlacementManagerDeleteFailed, int32(500), v1alpha1API.INTERNAL, "Placement Manager Error"),
			Entry("generic service error", errors.New("database error"), int32(500), v1alpha1API.INTERNAL, "Internal Server Error"),
		)
	})
})
