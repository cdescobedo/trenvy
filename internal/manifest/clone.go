package manifest

// Clone returns a copy of m that shares no map or pointer with it, made by
// encoding and re-parsing: the same path a save and a later load take, so
// the copy is exactly what would be read back from disk. commitStaged
// validates a whole batch on a clone before its first keyring write.
func (m *Manifest) Clone() (*Manifest, error) {
	b, err := m.Encode()
	if err != nil {
		return nil, err
	}
	return Parse(b)
}
