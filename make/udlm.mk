# UDLM domain (codegen): read API over the per-state records (docs/udlm-native.md).
UDLM_DOMAIN := udlm
UDLM_API := api/$(UDLM_DOMAIN)/v1alpha1
UDLM_SERVER_DIR := internal/$(UDLM_DOMAIN)/api/server

generate-udlm-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		--config=$(UDLM_API)/types.gen.cfg \
		-o $(UDLM_API)/types.gen.go \
		$(UDLM_API)/openapi.yaml

generate-udlm-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		--config=$(UDLM_API)/spec.gen.cfg \
		-o $(UDLM_API)/spec.gen.go \
		$(UDLM_API)/openapi.yaml

generate-udlm-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) \
		--config=$(UDLM_SERVER_DIR)/server.gen.cfg \
		-o $(UDLM_SERVER_DIR)/server.gen.go \
		$(UDLM_API)/openapi.yaml

generate-udlm-api: generate-udlm-types generate-udlm-spec generate-udlm-server

test-udlm:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(UDLM_DOMAIN)/...

.PHONY: generate-udlm-types generate-udlm-spec generate-udlm-server generate-udlm-api test-udlm
