package sp_subsystem_test_selfheal

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

agent_available(name) if {
	some a in input.available_agents
	a.name == name
}
