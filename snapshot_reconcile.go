package main

// Rebuild shared rate zones before any restore step validates Nginx. Historical
// snapshots include site references and policies but can omit the HTTP include.
// Reuse the policy validator/compiler so restoration preserves enforcement.
func reconcileSnapshotWAFHTTP(policyPath, httpPath string) error {
	policies, err := readWAFPolicies(policyPath)
	if err != nil {
		return err
	}
	return writeAtomic(httpPath, []byte(compileWAFHTTP(policies)), 0644)
}
