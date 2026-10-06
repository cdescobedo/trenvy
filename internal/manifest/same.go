package manifest

// SameVar reports whether importing incoming would change nothing about an
// existing variable. Re-running import against a sample that has not changed
// must not demand --force: there is nothing to overwrite, only the same
// value written back.
func SameVar(existing, incoming Var) bool {
	if existing.Secret != incoming.Secret {
		return false
	}
	if existing.Value == nil || incoming.Value == nil {
		return existing.Value == incoming.Value
	}
	return *existing.Value == *incoming.Value
}
