# Service provider domain (codegen and subsystem tests).
SP_DOMAIN := sp
SP_RM_API := api/$(SP_DOMAIN)/v1alpha1/resource_manager
SP_RM_SERVER_DIR := internal/$(SP_DOMAIN)/api/resource_manager
SP_RM_CLIENT_DIR := pkg/$(SP_DOMAIN)/client/resource_manager

generate-sp-rm-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_API)/types.gen.cfg \
		-o $(SP_RM_API)/types.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_API)/spec.gen.cfg \
		-o $(SP_RM_API)/spec.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_SERVER_DIR)/server.gen.cfg \
		-o $(SP_RM_SERVER_DIR)/server.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(SP_RM_CLIENT_DIR)/client.gen.cfg \
		-o $(SP_RM_CLIENT_DIR)/client.gen.go \
		$(SP_RM_API)/openapi.yaml

generate-sp-rm-api: generate-sp-rm-types generate-sp-rm-spec generate-sp-rm-server generate-sp-rm-client

generate-sp-api: generate-sp-rm-api

check-sp-aep-rm:
	spectral lint --fail-severity=warn ./$(SP_RM_API)/openapi.yaml

check-sp-aep: check-sp-aep-rm

test-sp:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(SP_DOMAIN)

sp-subsystem-test-up:
	$(COMPOSE) -f test/subsystem/$(SP_DOMAIN)/docker-compose.yaml up -d --build

sp-subsystem-test-down:
	$(COMPOSE) -f test/subsystem/$(SP_DOMAIN)/docker-compose.yaml down -v

sp-subsystem-test:
	$(GINKGO) $(GINKGO_FLAGS) -tags=subsystem ./test/subsystem/$(SP_DOMAIN)

.PHONY: generate-sp-rm-types generate-sp-rm-spec \
	generate-sp-rm-server generate-sp-rm-client generate-sp-rm-api generate-sp-api \
	check-sp-aep-rm check-sp-aep test-sp \
	sp-subsystem-test-up sp-subsystem-test-down sp-subsystem-test
