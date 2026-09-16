package sp_subsystem_test

default main := {"rejected": false}

main := {"rejected": false, "selected_agent": "__AGENT_NAME__"} if {
	some a in input.available_agents
	a.name == "__AGENT_NAME__"
}
