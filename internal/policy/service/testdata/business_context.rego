# Ask the information provider what business service a request belongs to, and
# whether a change freeze covers it. The request's name is the lookup key: it
# must equal the workload's handle in the estate.
#
# This policy decides and cites. It never rejects: a freeze defers a change and
# never refuses it, and the control plane cannot hold a request yet, so the
# freeze is stated in the reason and the request goes on. If the provider does
# not answer within two seconds the request goes on too, with nothing cited.
#
# `reason` and `consulted` are read by control planes built from
# croadfeldt/control-plane with the per-policy provenance change. An older
# build ignores both keys, and the policy is then harmless.
package policies.business_context

provider := "__PROVIDER__"

default main := {
	"rejected": false,
	"reason": "the information provider gave no answer; nothing was consulted",
}

answer := response.body if {
	response := http.send({
		"method": "POST",
		"url": concat("", [provider, "/search"]),
		"headers": {"Content-Type": "application/json"},
		"body": {"handle": input.spec.metadata.name},
		"timeout": "2s",
		"raise_error": false,
	})
	response.status_code == 200
}

main := {"rejected": false, "reason": reason} if {
	answer.matched == false
	reason := sprintf("no business service claims %s; nothing was consulted", [input.spec.metadata.name])
}

main := {"rejected": false, "reason": reason, "consulted": consulted} if {
	answer.matched == true
	service := answer.business_service
	reason := concat("; ", array.concat(
		[sprintf("business service %s, impact tier %s, owner %s", [service.handle, service.impact_tier, service.owner_group])],
		freeze_note,
	))
	consulted := array.concat(
		[{"uuid": service.uuid, "version": service.version, "head": service.integrity.head}],
		answer.freeze.calendars_read,
	)
}

freeze_note := [note] if {
	answer.freeze.frozen == true
	note := sprintf(
		"change freeze %s (%s) until %s; not held, the control plane cannot defer a request yet",
		[answer.freeze.name, answer.freeze.calendar_ref, answer.freeze.until],
	)
}

freeze_note := ["no change freeze covers it"] if {
	answer.freeze.frozen == false
}
