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

// Keep the canonical state for controls and health checks; timer activity is not
// proof that its one-shot updater is currently executing.
func timerServiceLabel(state, subState string) string {
	if state != "running" {
		return state
	}
	if subState == "waiting" {
		return "Scheduled / waiting"
	}
	return "Active timer"
}

func ftpsServiceUnit(exists func(string) bool) string {
	for _, unit := range []string{"litecp-ftps", "vsftpd", "pure-ftpd", "proftpd"} {
		if exists(unit) {
			return unit
		}
	}
	return ""
}

// Resource-enforced sites use a dedicated FPM instance, not the shared pool.
func phpHealthSocket(shared string, site Site, state resourceRuntimeStatus) string {
	if site.Suspended {
		return ""
	}
	if site.ResourceLimits == nil || !site.ResourceLimits.Enabled {
		return shared
	}
	if state.Enforced && state.State == "Resource blocked" {
		return ""
	}
	return resourceSocket(site.ID, "php")
}
