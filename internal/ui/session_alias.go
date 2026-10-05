package ui

import "github.com/cdescobedo/trenvy/internal/session"

// The matrix, the problems and the staged-change vocabulary moved to
// internal/session so internal/manage can share them. These aliases keep
// every existing caller -- this package, internal/cli and their tests --
// compiling unchanged until the cutover deletes this package.
type (
	Cell    = session.Cell
	Matrix  = session.Matrix
	Problem = session.Problem
	Change  = session.Change
)

const (
	CellShared   = session.CellShared
	CellOverride = session.CellOverride
	CellStored   = session.CellStored
	CellMissing  = session.CellMissing
	CellOptional = session.CellOptional

	StateFresh      = session.StateFresh
	StateAbsent     = session.StateAbsent
	StateStale      = session.StateStale
	StateModified   = session.StateModified
	StateForeign    = session.StateForeign
	StateUnknown    = session.StateUnknown
	StateUnverified = session.StateUnverified

	ChangeSetValue      = session.ChangeSetValue
	ChangeAddVar        = session.ChangeAddVar
	ChangeDeleteVar     = session.ChangeDeleteVar
	ChangeSetSecret     = session.ChangeSetSecret
	ChangePromoteSecret = session.ChangePromoteSecret
	ChangeAddOutput     = session.ChangeAddOutput
	ChangeRenameOutput  = session.ChangeRenameOutput
	ChangeRemoveOutput  = session.ChangeRemoveOutput
	ChangeDefaultOutput = session.ChangeDefaultOutput
	ChangeSetGlobs      = session.ChangeSetGlobs

	ChangeSetShared   = session.ChangeSetShared
	ChangeUnsetValue  = session.ChangeUnsetValue
	ChangeSetOptional = session.ChangeSetOptional

	ChangeSetOutputFormat = session.ChangeSetOutputFormat
	ChangeSetOutputPath   = session.ChangeSetOutputPath
)

var (
	Classify   = session.Classify
	NewMatrix  = session.NewMatrix
	Problems   = session.Problems
	Membership = session.Membership
)
