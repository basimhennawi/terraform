package terraform

import (
	"log"

	"github.com/hashicorp/terraform/internal/addrs"
	"github.com/hashicorp/terraform/internal/configs"
	"github.com/hashicorp/terraform/internal/states"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

// ValidateOpts are the various options that affect the details of how Terraform
// will validate a configuration.
type ValidateOpts struct {
	// LintChecks, if set to true, enables additional warnings that describe
	// ways to possibly improve a Terraform configuration even though the
	// current configuration is valid as written.
	//
	// The additional warnings produced in "lint" mode are more subjective and
	// so module authors can evaluate each one and choose to evaluate warnings
	// that don't apply in some particular situations. There might be additional
	// lint warnings in later releases, thus making a previously-lint-free
	// configuration potentially lint-y again, and so considering lint warnings
	// should typically be a development task in its own right, rather than a
	// blocker for completing other development tasks.
	LintChecks bool
}

// DefaultValidateOpts is a reasonable default set of validate options to use
// in common cases without any special needs.
var DefaultValidateOpts = &ValidateOpts{
	LintChecks: false,
}

// Validate performs semantic validation of a configuration, and returns
// any warnings or errors.
//
// Syntax and structural checks are performed by the configuration loader,
// and so are not repeated here.
//
// Validate considers only the configuration and so it won't catch any
// errors caused by current values in the state, or other external information
// such as root module input variables. However, the Plan function includes
// all of the same checks as Validate, in addition to the other work it does
// to consider the previous run state and the planning options.
//
// Don't modify anything reachable through the arguments after calling this
// function.
func (c *Context) Validate(config *configs.Config, opts *ValidateOpts) tfdiags.Diagnostics {
	defer c.acquireRun("validate")()

	var diags tfdiags.Diagnostics

	moreDiags := CheckCoreVersionRequirements(config)
	diags = diags.Append(moreDiags)
	// If version constraints are not met then we'll bail early since otherwise
	// we're likely to just see a bunch of other errors related to
	// incompatibilities, which could be overwhelming for the user.
	if diags.HasErrors() {
		return diags
	}

	log.Printf("[DEBUG] Building and walking validate graph")

	graph, moreDiags := validateGraphBuilder(&PlanGraphBuilder{
		Config:   config,
		Plugins:  c.plugins,
		Validate: true,
		State:    states.NewState(),
	}, opts).Build(addrs.RootModuleInstance)
	diags = diags.Append(moreDiags)
	if moreDiags.HasErrors() {
		return diags
	}

	// Validate is to check if the given module is valid regardless of
	// input values, current state, etc. Therefore we populate all of the
	// input values with unknown values of the expected type, allowing us
	// to perform a type check without assuming any particular values.
	varValues := make(InputValues)
	for name, variable := range config.Module.Variables {
		ty := variable.Type
		if ty == cty.NilType {
			// Can't predict the type at all, so we'll just mark it as
			// cty.DynamicVal (unknown value of cty.DynamicPseudoType).
			ty = cty.DynamicPseudoType
		}
		varValues[name] = &InputValue{
			Value:      cty.UnknownVal(ty),
			SourceType: ValueFromUnknown,
		}
	}

	walker, walkDiags := c.walk(graph, walkValidate, &graphWalkOpts{
		Config:             config,
		RootVariableValues: varValues,
	})
	diags = diags.Append(walker.NonFatalDiagnostics)
	diags = diags.Append(walkDiags)
	if walkDiags.HasErrors() {
		return diags
	}

	return diags
}
