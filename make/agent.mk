# Agent domain (codegen).
AGENT_DOMAIN := agent
AGENT_API := api/$(AGENT_DOMAIN)/v1alpha1
AGENT_SERVER_DIR := internal/$(AGENT_DOMAIN)/api/server

generate-agent-types:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(AGENT_API)/types.gen.cfg \
		-o $(AGENT_API)/types.gen.go \
		$(AGENT_API)/openapi.yaml

generate-agent-spec:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(AGENT_API)/spec.gen.cfg \
		-o $(AGENT_API)/spec.gen.go \
		$(AGENT_API)/openapi.yaml

generate-agent-server:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(AGENT_SERVER_DIR)/server.gen.cfg \
		-o $(AGENT_SERVER_DIR)/server.gen.go \
		$(AGENT_API)/openapi.yaml

AGENT_CLIENT_DIR := pkg/$(AGENT_DOMAIN)/client

generate-agent-client:
	go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
		--config=$(AGENT_CLIENT_DIR)/client.gen.cfg \
		-o $(AGENT_CLIENT_DIR)/client.gen.go \
		$(AGENT_API)/openapi.yaml

generate-agent-api: generate-agent-types generate-agent-spec generate-agent-server generate-agent-client

test-agent:
	$(GINKGO) $(GINKGO_FLAGS) ./internal/$(AGENT_DOMAIN)/...

.PHONY: generate-agent-types generate-agent-spec generate-agent-server generate-agent-client generate-agent-api test-agent
