package session

// Change kinds. Task 9 switches on Kind to decide where a change lands: the
// manifest for set-value/add-var/delete-var, the keyring for set-secret.
//
// ChangePromoteSecret is `s`'s row-level record: it stands apart from the
// set-secret changes `s` stages alongside it (see promoteSecret) because a
// set-secret can also come from editing a cell that was already secret,
// which must not flip anything in the manifest. Only ChangePromoteSecret
// tells the phased commit (2a's Task 3) "this variable becomes secret ->
// flip the manifest entry to secret = true with no Value and no Profiles".
//
// The four Change*Output kinds are the Outputs screen's own staged edits (Task
// 5). They key by the output's identity at the start of the session (or the
// name it was added under this session), never by a pending rename's
// target, so a row that is both renamed and,
// say, made default in the same session stages two Changes that agree about
// which row they mean.
//
// ChangeSetGlobs is Task 8's own addition, keyed by identity the same way:
// the include/exclude editor's save. It did not exist when Task 6 built
// commitStaged's phases, so wiring it in means adding a case to whichever
// phase it belongs in, never restructuring the phases themselves (see
// commitStaged's own doc in internal/cli/ui.go).
const (
	ChangeSetValue      = "set-value"
	ChangeAddVar        = "add-var"
	ChangeDeleteVar     = "delete-var"
	ChangeSetSecret     = "set-secret"
	ChangePromoteSecret = "promote-secret"
	ChangeAddOutput     = "add-output"
	ChangeRenameOutput  = "rename-output"
	ChangeRemoveOutput  = "remove-output"
	ChangeDefaultOutput = "default-output"
	ChangeSetGlobs      = "set-globs"
)

// Change is one uncommitted edit, held in the model until w writes it to the
// manifest, the state file or the keyring (Task 6). Profile says which
// column a set-value or set-secret belongs to; a value written to the wrong
// profile would be silent data corruption, so the field is never left to be
// inferred from cursor position at commit time.
//
// Value also carries rename-output's target name: the name the output
// becomes, reusing the "what this change establishes" meaning it already
// has for set-value and set-secret rather than growing a field that would
// mean the same thing under a different name for one kind.
//
// Format and Path carry add-output's definition; every other kind leaves
// them empty.
//
// Include and Exclude carry set-globs' own definition: exactly what
// WriteGlobs produced from the editor's checkbox state and mode at the
// moment enter saved it. Every other kind leaves them nil.
type Change struct {
	Kind, Name, Profile, Value string
	Format, Path               string
	Include, Exclude           []string
}

// Kinds added for internal/manage (spec 2026-09-30). The original interface
// never stages them; internal/cli's commitStaged applies them in phase 5a
// with the other manifest edits.
const (
	// ChangeSetShared sets a plain variable's shared Value -- the value every
	// profile without an override resolves to. ChangeSetValue, by contrast,
	// always writes one profile's override.
	ChangeSetShared = "set-shared"
	// ChangeUnsetValue removes a plain value: the shared Value when Profile
	// is "", otherwise that profile's override.
	ChangeUnsetValue = "unset-value"
	// ChangeSetOptional sets Optional. Value is "true" or "false".
	ChangeSetOptional = "set-optional"

	// ChangeSetOutputFormat and ChangeSetOutputPath edit an output's format
	// or path in place. Value is the new one. Like every output kind they
	// key by the output's identity (see Staged), never by a staged rename's
	// target, and commitStaged applies them in phase 5a, before 5b renames.
	ChangeSetOutputFormat = "set-output-format"
	ChangeSetOutputPath   = "set-output-path"
)
