package cli

import (
	"path/filepath"

	"github.com/izzzzzi/agent-assh/internal/audit"
	"github.com/izzzzzi/agent-assh/internal/safety"
	"github.com/spf13/cobra"
)

// classifyCommand runs the built-in safety classifier plus the optional deny-only
// policy overlay. It returns the classification result. If the policy file is
// present but invalid it fails closed: handled is true and a typed error has
// already been written to cmd, so the caller must return errReturn immediately.
//
// When a policy rule fires, the policy file path and content hash are recorded in
// the audit trail so operators can see which policy blocked a command.
func classifyCommand(cmd *cobra.Command, command string) (result safety.Result, handled bool, errReturn error) {
	policy, perr := safety.LoadPolicy(safety.DefaultPolicyPath())
	if perr != nil {
		var pe *safety.PolicyError
		if e, ok := perr.(*safety.PolicyError); ok {
			pe = e
		}
		if pe != nil {
			return safety.Result{}, true, writeError(cmd, pe.Code, pe.Message, pe.Hint)
		}
		return safety.Result{}, true, writeError(cmd, "safety_policy_invalid", perr.Error(), "")
	}

	res := safety.CheckCommandWithPolicy(command, policy)
	// Only log a policy audit event when a deny-only policy rule (not a built-in)
	// was the one that fired. Built-in blocks logged separately by the caller.
	if IsPolicyBlock(res, policy) {
		_ = audit.Write(filepath.Join(stateBaseDir(), "audit", "audit.jsonl"), audit.Event{
			Action:           "safety_policy_block",
			SafetyRule:       res.Rule,
			SafetyPolicyPath: policy.Path(),
			SafetyPolicyHash: policy.SHA256(),
		})
	}
	return res, false, nil
}

// IsPolicyBlock returns true when the safety result was caused by a deny-only
// policy file rule, not by a built-in rule.
func IsPolicyBlock(res safety.Result, policy *safety.Policy) bool {
	if policy == nil {
		return false
	}
	if !res.Dangerous {
		return false
	}
	if len(res.Rule) > 12 && res.Rule[:12] == "policy_deny:" {
		return true
	}
	return false
}
