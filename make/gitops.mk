# GitOps domain (codegen and subsystem tests).
GITOPS_DOMAIN := gitops
GITOPS_API := api/$(GITOPS_DOMAIN)/v1alpha1
GITOPS_SERVER_DIR := internal/$(GITOPS_DOMAIN)/api/server
GITOPS_CLIENT_DIR := pkg/$(GITOPS_DOMAIN)/client

generate-gitops-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(GITOPS_API)/types.gen.cfg \
		-o $(GITOPS_API)/types.gen.go \
		$(GITOPS_API)/openapi.yaml

generate-gitops-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(GITOPS_API)/spec.gen.cfg \
		-o $(GITOPS_API)/spec.gen.go \
		$(GITOPS_API)/openapi.yaml

generate-gitops-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(GITOPS_SERVER_DIR)/server.gen.cfg \
		-o $(GITOPS_SERVER_DIR)/server.gen.go \
		$(GITOPS_API)/openapi.yaml

generate-gitops-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(GITOPS_CLIENT_DIR)/client.gen.cfg \
		-o $(GITOPS_CLIENT_DIR)/client.gen.go \
		$(GITOPS_API)/openapi.yaml

generate-gitops-api: generate-gitops-types generate-gitops-spec generate-gitops-server generate-gitops-client

test-gitops:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(GITOPS_DOMAIN) ./pkg/$(GITOPS_DOMAIN)

gitops-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(GITOPS_DOMAIN)/docker-compose.yaml up -d --build

gitops-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(GITOPS_DOMAIN)/docker-compose.yaml down -v

gitops-subsystem-test:
	$(GINKGO) $(GINKGO_FLAGS) -tags=subsystem ./test/subsystem/$(GITOPS_DOMAIN)

.PHONY: generate-gitops-types generate-gitops-spec \
	generate-gitops-server generate-gitops-client generate-gitops-api \
	test-gitops gitops-subsystem-test-up gitops-subsystem-test-down \
	gitops-subsystem-test
