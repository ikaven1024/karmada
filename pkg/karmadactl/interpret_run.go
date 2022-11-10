package karmadactl

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/karmada-io/karmada/pkg/resourceinterpreter/configurableinterpreter"
)

type nameValue struct {
	Name  string
	Value interface{}
}

type ruleResult struct {
	Results []nameValue
	Err     error
}

func newRuleResult() *ruleResult {
	return &ruleResult{}
}

func newRuleResultWithError(err error) *ruleResult {
	return &ruleResult{
		Err: err,
	}
}

func (r *ruleResult) add(name string, value interface{}) *ruleResult {
	r.Results = append(r.Results, nameValue{Name: name, Value: value})
	return r
}

func (o *InterpretOptions) runTest() error {
	if o.Operation == "" {
		return fmt.Errorf("operation is not set for testing")
	}

	customizations, err := o.getCustomizationObject()
	if err != nil {
		return err
	}

	desired, err := o.getDesiredObject()
	if err != nil {
		return err
	}

	observed, err := o.getObservedObject()
	if err != nil {
		return err
	}

	status, err := o.getAggregatedStatusItems()
	if err != nil {
		return err
	}

	args := ruleArgs{
		Desired:  desired,
		Observed: observed,
		Status:   status,
		Replica:  int64(o.DesiredReplica),
	}

	interpreter := configurableinterpreter.NewConfigurableInterpreterWithoutInformer()
	err = interpreter.LoadConfig(customizations)
	if err != nil {
		return err
	}

	r := o.Rules.Get(o.Operation)
	if r == nil {
		return fmt.Errorf("operation %s is not supported. Use one of: %s", o.Operation, strings.Join(o.Rules.Names(), ", "))
	}

	result := r.Run(interpreter, args)
	printResult(o.Out, o.ErrOut, r.Name(), result)
	return nil
}

func printResult(w, errOut io.Writer, name string, result *ruleResult) {
	fmt.Fprintf(w, "# %s RESULTS:\n", name)
	if result.Err != nil {
		fmt.Fprintf(errOut, "ERROR: %v\n", result.Err)
		return
	}

	for _, res := range result.Results {
		func() {
			// If we put newEncoder() before for loop, we will get:
			//    ---
			//    # RESULT1
			//    foo:
			//    # RESULT2
			//    ---
			//    bar:
			//
			// Whereas we want:
			//    ---
			//    # RESULT1
			//    foo:
			//    ---
			//    # RESULT2
			//    bar:
			encoder := yaml.NewEncoder(w)
			defer encoder.Close()
			fmt.Fprintln(w, "---")
			fmt.Fprintln(w, "# "+strings.ToUpper(res.Name))
			if err := encoder.Encode(res.Value); err != nil {
				fmt.Fprintf(errOut, "ERROR: %v\n", err)
			}
		}()
	}
}
