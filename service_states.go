package main

func normalizeServiceState(state string) string {
	switch state {
	case "active":
		return "running"
	case "inactive":
		return "stopped"
	case "failed", "activating", "deactivating", "reloading":
		return state
	default:
		return "unknown"
	}
}

func aggregateServiceStates(states []string) string {
	if len(states) == 0 {
		return "not installed"
	}
	first := states[0]
	for _, state := range states[1:] {
		if state != first {
			return "degraded"
		}
	}
	return first
}
