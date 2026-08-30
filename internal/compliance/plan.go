package compliance

// ActionKind names what an action does. Apply switches on it; the report prints
// the description beside it.
type ActionKind string

// Every action forgectl can plan.
const (
	ActionRenameBranch     ActionKind = "rename-branch"
	ActionPushBranch       ActionKind = "push-branch"
	ActionSetDefaultBranch ActionKind = "set-default-branch"
	ActionSetRemoteHead    ActionKind = "set-remote-head"
	ActionDeleteOldBranch  ActionKind = "delete-old-branch"
	ActionSetProtection    ActionKind = "set-protection"
	ActionProtectTag       ActionKind = "protect-tag"
	ActionSetVariable      ActionKind = "set-variable"
	ActionRotateToken      ActionKind = "rotate-token"
)

// String renders the action kind as it appears in the machine-readable output.
func (k ActionKind) String() string { return string(k) }

// Action is one step apply intends to take.
//
// Description is the line shown in the plan preview. Like every message field
// in this package it describes state and MUST NOT contain a value (FR-054).
type Action struct {
	Kind   ActionKind
	Domain Domain

	// Description is the human-readable line of the plan preview.
	Description string
	// Destructive drives the confirmation of CLI-003.
	Destructive bool

	// Target is what the action operates on: a branch name, a tag pattern, or a
	// variable name. It is never a value.
	Target string
	// From is the branch an action moves away from, for the rename and delete
	// steps.
	From string
}

// Plan is the ordered list of actions apply intends to perform, shown before
// anything is changed.
//
// An empty plan means a compliant repository: no confirmation is requested and
// no mutating platform call is made (FR-035).
type Plan struct {
	Actions []Action
}

// Empty reports whether the plan would change nothing.
func (p *Plan) Empty() bool { return len(p.Actions) == 0 }

// Destructive reports whether any action needs confirmation (CLI-003).
func (p *Plan) Destructive() bool {
	for _, a := range p.Actions {
		if a.Destructive {
			return true
		}
	}

	return false
}

// Add appends an action.
func (p *Plan) Add(a Action) { p.Actions = append(p.Actions, a) }

// Filter returns the plan restricted to the given domains. Passing no domain
// returns the plan unchanged, so the caller need not special-case "no
// restriction" (FR-036).
func (p *Plan) Filter(only, skip []Domain) Plan {
	keep := func(d Domain) bool {
		if len(only) > 0 && !containsDomain(only, d) {
			return false
		}

		return !containsDomain(skip, d)
	}

	out := Plan{}
	for _, a := range p.Actions {
		if keep(a.Domain) {
			out.Add(a)
		}
	}

	return out
}

// containsDomain reports whether a domain is in the list.
func containsDomain(list []Domain, d Domain) bool {
	for _, item := range list {
		if item == d {
			return true
		}
	}

	return false
}

// ActionStatus is what became of a planned action once apply ran it.
type ActionStatus string

// The outcomes an executed action can have. A run that stops partway leaves
// later actions "skipped", which is what makes the partial-failure report of
// FR-045 readable.
const (
	ActionDone    ActionStatus = "done"
	ActionFailed  ActionStatus = "failed"
	ActionSkipped ActionStatus = "skipped"
)

// String renders the action status as it appears in output.
func (s ActionStatus) String() string { return string(s) }

// ActionResult is one executed action, with what became of it.
//
// Error carries the failure message, which like every message in this package
// is rendered without any value (FR-054).
type ActionResult struct {
	Action

	Status ActionStatus
	Error  string
}

// VariableTargets lists the variables this plan will write, so their values can
// all be resolved before the first write rather than one at a time (FR-044).
func (p *Plan) VariableTargets() []string {
	var names []string

	for _, a := range p.Actions {
		if a.Kind == ActionSetVariable {
			names = append(names, a.Target)
		}
	}

	return names
}
