#!/usr/bin/env bash
set -euo pipefail

chart_dir="${1:?usage: $0 <chart-dir>}"
release="dcm"

fail() {
	echo "$*" >&2
	exit 1
}

helm_out() {
	helm template "$release" "$chart_dir" "--skip-schema-validation" "$@"
}

yaml_block() {
	local awk_expr="$1"
	printf '%s' "$2" | awk "$awk_expr"
}

require_block() {
	local name="$1"
	local awk_expr="$2"
	shift 2
	local out block
	out="$(helm_out "$@")"
	block="$(yaml_block "$awk_expr" "$out")"
	if [ -z "$block" ]; then
		fail "missing $name"
	fi
	printf '%s' "$block"
}

require_template_failure() {
	local name="$1"
	local msg="$2"
	shift 2
	local out ec=0
	out="$(helm_out "$@" 2>&1)" || ec=$?
	if [ "$ec" -eq 0 ]; then
		fail "expected helm template to fail for $name"
	fi
	if ! printf '%s\n' "$out" | grep -Fq "$msg"; then
		echo "unexpected error for $name:" >&2
		printf '%s\n' "$out" >&2
		exit 1
	fi
}

helm_out >/dev/null

issuer_url="https://keycloak.example.com/realms/dcm"

block="$(require_block "keycloak-realm manifest in helm template output" 'BEGIN{RS="---"} /keycloak-realm/ {print; exit}' --set auth.enabled=true)"
printf '%s' "$block" | grep -q '^kind: Secret$' || fail "keycloak-realm must render as Secret"

require_block "Keycloak Route manifest when auth.keycloak.route.enabled=true" 'BEGIN{RS="---"} /templates\/keycloak.yaml/ && /kind: Route/ {print; exit}' --set auth.enabled=true --set auth.issuerURL="$issuer_url" --set auth.keycloak.route.enabled=true >/dev/null

require_block "Keycloak Ingress manifest when auth.keycloak.ingress.enabled=true" 'BEGIN{RS="---"} /templates\/keycloak.yaml/ && /kind: Ingress/ {print; exit}' --set auth.enabled=true --set auth.issuerURL="$issuer_url" --set auth.keycloak.ingress.enabled=true >/dev/null

require_template_failure "auth enabled without authSecretRef" "auth.authSecretRef is required when auth.enabled=true" --set auth.enabled=true --set auth.authSecretRef=

require_template_failure "route without auth.issuerURL" "auth.issuerURL is required when auth.keycloak.route.enabled=true" --set auth.enabled=true --set auth.keycloak.route.enabled=true

require_template_failure "invalid auth.issuerURL suffix" "auth.issuerURL must end with /realms/dcm" --set auth.enabled=true --set auth.issuerURL="https://keycloak.example.com"

require_template_failure "ingress without auth.issuerURL" "auth.issuerURL is required when auth.keycloak.ingress.enabled=true" --set auth.enabled=true --set auth.keycloak.ingress.enabled=true

auth_ref_out="$(helm_out --set auth.enabled=true --set auth.authSecretRef=my-auth)"
if printf '%s' "$auth_ref_out" | awk 'BEGIN{RS="---"} /kind: Secret/ && /name: dcm-auth/ {found=1} END{exit !found}'; then
	fail "chart auth Secret must not render when using external authSecretRef"
fi
auth_ref_count="$(printf '%s' "$auth_ref_out" | grep -c 'name: my-auth')"
[ "$auth_ref_count" -eq 5 ] || fail "pods must reference auth.authSecretRef in all 5 secretKeyRef entries (found $auth_ref_count)"

db_ref_out="$(helm_out)"
if printf '%s' "$db_ref_out" | awk 'BEGIN{RS="---"} /kind: Secret/ && /name: dcm-db/ && /stringData/ {found=1} END{exit !found}'; then
	fail "chart db Secret must not render; use postgres.dbSecretRef"
fi
db_ref_count="$(printf '%s' "$db_ref_out" | grep -c 'name: dcm-db')"
[ "$db_ref_count" -ge 2 ] || fail "workloads must reference postgres.dbSecretRef (found $db_ref_count)"

# Environment agent
require_block "environment-agent Deployment when enabled" 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Deployment/ {print; exit}' --set environmentAgent.enabled=true --set environmentAgent.embeddedSps=container >/dev/null
require_block "environment-agent ServiceAccount when enabled" 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: ServiceAccount/ {print; exit}' --set environmentAgent.enabled=true --set environmentAgent.embeddedSps=container >/dev/null
require_block "environment-agent workload Role when enabled" 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Role/ && /environment-agent-workloads/ {print; exit}' --set environmentAgent.enabled=true --set environmentAgent.embeddedSps=container >/dev/null

ea_out="$(helm_out --set environmentAgent.enabled=true --set environmentAgent.embeddedSps=container)"
if printf '%s' "$ea_out" | grep -Fq 'SP_DEFAULT_KUBECONFIG'; then
	fail "environment-agent must not set SP_DEFAULT_KUBECONFIG (in-cluster auth only)"
fi
if printf '%s' "$ea_out" | grep -Fq 'mountPath: /kubeconfig'; then
	fail "environment-agent must not mount a kubeconfig"
fi
printf '%s' "$ea_out" | grep -Fq 'DCM_REGISTRATION_URL' || fail "environment-agent must set DCM_REGISTRATION_URL"
printf '%s' "$ea_out" | grep -Fq 'http://dcm-control-plane:8080' || fail "environment-agent DCM_REGISTRATION_URL must be the control-plane base URL"
if printf '%s' "$ea_out" | awk 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Deployment/ && /emptyDir:/ {found=1} END{exit !found}'; then
	fail "environment-agent must not use emptyDir for registrations"
fi
printf '%s' "$ea_out" | awk 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: PersistentVolumeClaim/ && /environment-agent-data/ {found=1} END{exit !found}' \
	|| fail "environment-agent must render a registrations PVC"

require_template_failure "environment-agent cluster without pullSecretRef" \
	"environmentAgent.pullSecretRef is required when embeddedSps includes cluster" \
	--set environmentAgent.enabled=true --set environmentAgent.embeddedSps=cluster --set environmentAgent.pullSecretRef=

cluster_out="$(helm_out --set environmentAgent.enabled=true --set "environmentAgent.embeddedSps=container\,cluster" --set environmentAgent.pullSecretRef=acm-pull-secret)"
printf '%s' "$cluster_out" | awk 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Role/ && /environment-agent-cluster/ {found=1} END{exit !found}' \
	|| fail "missing environment-agent cluster Role when cluster embedded"
printf '%s' "$cluster_out" | awk 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Deployment/ && /name: SP_PULL_SECRET/ && /name: acm-pull-secret/ {found=1} END{exit !found}' \
	|| fail "environment-agent cluster Deployment must reference pullSecretRef via SP_PULL_SECRET"

# Separate-namespace Roles for vm / storage / network
require_block "environment-agent vm Role when vmNamespace differs" \
	'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Role/ && /environment-agent-vm/ && /namespace: kubevirt/ {print; exit}' \
	--set environmentAgent.enabled=true --set environmentAgent.embeddedSps=vm \
	--set environmentAgent.vmNamespace=kubevirt --set environmentAgent.containerNamespace=default >/dev/null

require_block "environment-agent storage Role when storageNamespace differs" \
	'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Role/ && /environment-agent-storage/ && /namespace: storage-ns/ {print; exit}' \
	--set environmentAgent.enabled=true --set environmentAgent.embeddedSps=storage \
	--set environmentAgent.storageNamespace=storage-ns --set environmentAgent.containerNamespace=default >/dev/null

require_block "environment-agent network Role when networkNamespace differs" \
	'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Role/ && /environment-agent-network/ && /namespace: net-ns/ {print; exit}' \
	--set environmentAgent.enabled=true --set environmentAgent.embeddedSps=network \
	--set environmentAgent.networkNamespace=net-ns --set environmentAgent.containerNamespace=default >/dev/null

storage_net_out="$(helm_out --set environmentAgent.enabled=true --set "environmentAgent.embeddedSps=storage\,network")"
printf '%s' "$storage_net_out" | grep -Fq 'SP_STORAGE_NAMESPACE' || fail "environment-agent must set SP_STORAGE_NAMESPACE for storage SP"
printf '%s' "$storage_net_out" | grep -Fq 'SP_NETWORK_NAMESPACE' || fail "environment-agent must set SP_NETWORK_NAMESPACE for network SP"

# Environment-agent DCM auth when auth.enabled
ea_auth_out="$(helm_out --set environmentAgent.enabled=true --set environmentAgent.embeddedSps=container --set auth.enabled=true)"
printf '%s' "$ea_auth_out" | grep -Fq 'DCM_AUTH_TOKEN_ENDPOINT' || fail "environment-agent must set DCM_AUTH_TOKEN_ENDPOINT when auth.enabled"
printf '%s' "$ea_auth_out" | grep -Fq 'DCM_AUTH_CLIENT_ID' || fail "environment-agent must set DCM_AUTH_CLIENT_ID when auth.enabled"
printf '%s' "$ea_auth_out" | grep -Fq 'DCM_AUTH_CLIENT_SECRET' || fail "environment-agent must set DCM_AUTH_CLIENT_SECRET when auth.enabled"
printf '%s' "$ea_auth_out" | grep -Fq 'openid-connect/token' || fail "environment-agent DCM_AUTH_TOKEN_ENDPOINT must be the Keycloak token URL"
printf '%s' "$ea_auth_out" | awk 'BEGIN{RS="---"} /templates\/environment-agent.yaml/ && /kind: Deployment/ && /key: AUTH_PROXY_SECRET/ {found=1} END{exit !found}' \
	|| fail "environment-agent must reference AUTH_PROXY_SECRET for DCM_AUTH_CLIENT_SECRET"
if printf '%s' "$ea_out" | grep -Fq 'DCM_AUTH_TOKEN_ENDPOINT'; then
	fail "environment-agent must not set DCM_AUTH_* when auth.enabled=false"
fi
