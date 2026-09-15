package sp_subsystem_test_three_agent

default main := {"rejected": false}

main := {"rejected": false, "selected_agent": "__AGENT_A__"} if {
	some a in input.available_agents
	a.name == "__AGENT_A__"
}

main := {"rejected": false, "selected_agent": "__AGENT_B__"} if {
	not agent_available("__AGENT_A__")
	some a in input.available_agents
	a.name == "__AGENT_B__"
}

# Unconditional: available_agents is already pre-filtered by service-type
# capability before Rego runs, so this only ever matches when evaluating a
# "database" resource (the only service type agentC is registered for).
main := {"rejected": false, "selected_agent": "__AGENT_C__"} if {
	some a in input.available_agents
	a.name == "__AGENT_C__"
}

agent_available(name) if {
	some a in input.available_agents
	a.name == name
}
