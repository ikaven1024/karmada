package karmadactl

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/kubectl/pkg/util/templates"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/karmadactl/util"
	"github.com/karmada-io/karmada/pkg/karmadactl/util/genericresource"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

var (
	interpretLong = templates.LongDesc(`
        Check the valid of customizations. Or show the result of the rules running.


        1. Check valid. Given the customization by NAME or '-f' option, this tool will
           check the rules in customization, and print 'PASS' or 'FAIL' for each rule.
           The fail reasons are also printed.


        2. Show result. Given the customization by NAME or '-f' option, the operation by
           'operation' option, and arguments by options(desired, desired-file, observed,
           observed-file, status, status-file, desired-replica),  this tool will run the
           rule and print result. The arguments for operations see below.

        - retention                : (desired|desired-file), (observed|observed-file)
        - replicaResource          : (desired|desired-file|observed|observed-file)
        - replicaRevision          : (desired|desired-file|observed|observed-file), desired-replica
        - statusReflection         : (desired|desired-file|observed|observed-file)
        - statusAggregation        : (desired|desired-file|observed|observed-file), status-file...
        - healthInterpretation     : (desired|desired-file|observed|observed-file)
        - dependencyInterpretation : (desired|desired-file|observed|observed-file)

`)

	interpretExample = templates.Examples(`
        # Check the customizations in file
        %[1]s interpret -f customization.json --check

        # Check the customizations on server
        %[1]s interpret customization --check

        # Check the retention rule
        %[1]s interpret customization --operation retention --check
    
        # Test the retention rule for local resource
        %[1]s interpret customization --operation retention --desired-file desired.json --observed-file observed.json

        # Test the retention rule for server resource
        %[1]s interpret customization --operation retention --desired-file desired.json --observed deployments/foo`)
)

// NewCmdInterpret new interpret command.
func NewCmdInterpret(f util.Factory, parentCommand string, streams genericclioptions.IOStreams) *cobra.Command {
	o := &InterpretOptions{
		IOStreams: streams,
		Rules:     defaultRules,
	}
	cmd := &cobra.Command{
		Use:                   "interpreter (-f FILENAME | NAME) (--operation OPERATION) [-n NAMESPACE] [--ARGS VALUE]... ",
		Short:                 "Check the valid of customizations. Or show the result of the rules running",
		Long:                  interpretLong,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		Example:               fmt.Sprintf(interpretExample, parentCommand),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.Complete(f, cmd, args); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
		Annotations: map[string]string{
			util.TagCommandGroup: util.GroupClusterTroubleshootingAndDebugging,
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&o.Operation, "operation", o.Operation, "The interpret operation to use. One of: ("+strings.Join(o.Rules.Names(), ",")+")")
	flags.StringVarP(&o.Filename, "filename", "f", o.Filename, "Filename, directory, or URL to files identifying the resource to fetch the customization rules.")
	flags.BoolVar(&o.Check, "check", o.Check, "Check the given customizations")
	flags.StringVar(&o.Desired, "desired", o.Desired, "The type and name of resource on server to use as desiredObj argument in rule script. For example: deployment/foo.")
	flags.StringVar(&o.DesiredFile, "desired-file", o.DesiredFile, "Filename, directory, or URL to files identifying the resource to use as desiredObj argument in rule script.")
	flags.StringVar(&o.Observed, "observed", o.Observed, "The type and name of resource on server to use as observedObj argument in rule script. For example: deployment/foo.")
	flags.StringVar(&o.ObservedFile, "observed-file", o.ObservedFile, "Filename, directory, or URL to files identifying the resource to use as observedObj argument in rule script.")
	flags.StringSliceVar(&o.StatusFile, "status-file", o.StatusFile, "Filename, directory, or URL to files identifying the resource to use as statusItems argument in rule script.")
	flags.Int32Var(&o.DesiredReplica, "desired-replica", o.DesiredReplica, "The desiredReplica argument in rule script.")
	// global flags
	flags.StringVar(defaultConfigFlags.KubeConfig, "kubeconfig", *defaultConfigFlags.KubeConfig, "Path to the kubeconfig file to use for CLI requests.")
	flags.StringVar(defaultConfigFlags.Context, "karmada-context", *defaultConfigFlags.Context, "The name of the kubeconfig context to use")
	flags.StringVarP(defaultConfigFlags.Namespace, "namespace", "n", *defaultConfigFlags.Namespace, "If present, the namespace scope for this CLI request")

	return cmd
}

// InterpretOptions contains the input to the interpret command.
type InterpretOptions struct {
	Operation string
	Filename  string
	Check     bool

	// args
	Desired        string
	DesiredFile    string
	Observed       string
	ObservedFile   string
	StatusFile     []string
	DesiredReplica int32

	CustomizationResult *resource.Result
	DesiredResult       *resource.Result
	ObservedResult      *resource.Result
	StatusResult        *genericresource.Result

	Rules rules

	genericclioptions.IOStreams
}

// Validate checks that the provided interpret options are specified.
func (o *InterpretOptions) Validate(args []string) error {
	// validate customization args and options.
	// customization can only be specified once by args or filename option.
	// And contains no slash.
	customizationCount := len(args)
	if o.Filename != "" {
		customizationCount++
	}
	switch customizationCount {
	case 0:
		return fmt.Errorf("on customization specified by by args or filename option")
	case 1:
		if len(args) > 0 && strings.Contains(args[0], "/") {
			return fmt.Errorf("it's no neeed to specify resource type for customization")
		}
	default:
		return fmt.Errorf("you can only specify one customization by args and filename option")
	}

	if o.Check {
		// validate options for check.
		if o.Operation != "" ||
			o.Desired != "" || o.DesiredFile != "" ||
			o.Observed != "" || o.ObservedFile != "" ||
			len(o.StatusFile) > 0 ||
			o.DesiredReplica != 0 {
			return fmt.Errorf("when option check is set, these options are not allowed: " +
				"operation, desired, desired-file, observed, observed-file, status-file, desired-replica")
		}
	} else {
		// validate options for testing.
		if o.Operation == "" {
			return fmt.Errorf("")
		}
	}

	return nil
}

// Complete ensures that options are valid and marshals them if necessary
func (o *InterpretOptions) Complete(f util.Factory, cmd *cobra.Command, args []string) error {
	cmdNamespace, enforceNamespace, err := f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	getResource := func(typeName string, filenames ...string) *resource.Result {
		filenameOptions := &resource.FilenameOptions{}
		for _, filename := range filenames {
			if filename != "" {
				filenameOptions.Filenames = append(filenameOptions.Filenames, filename)
			}
		}

		var nameArgs []string
		if typeName != "" {
			nameArgs = []string{typeName}
		}

		r := f.NewBuilder().
			Unstructured().
			NamespaceParam(cmdNamespace).
			DefaultNamespace().
			FilenameParam(enforceNamespace, filenameOptions).
			ResourceTypeOrNameArgs(false, nameArgs...).RequireObject(true).
			Do()
		return r
	}

	var customizationName string
	if len(args) > 0 {
		customizationName = customizationResourceType + "/" + args[0]
	}
	o.CustomizationResult = getResource(customizationName, o.Filename)
	if err = o.CustomizationResult.Err(); err != nil {
		return err
	}

	if o.Desired != "" || o.DesiredFile != "" {
		o.DesiredResult = getResource(o.Desired, o.DesiredFile)
		if err = o.DesiredResult.Err(); err != nil {
			return err
		}
	}

	if o.Observed != "" || o.ObservedFile != "" {
		o.ObservedResult = getResource(o.Observed, o.ObservedFile)
		if err = o.ObservedResult.Err(); err != nil {
			return err
		}
	}

	if len(o.StatusFile) > 0 {
		o.StatusResult = genericresource.NewBuilder().
			Constructor(func() interface{} { return workv1alpha2.AggregatedStatusItem{} }).
			Filename(true, o.StatusFile...).
			Do()
		if err = o.StatusResult.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Run describe information of resources
func (o *InterpretOptions) Run() error {
	if o.Check {
		return o.runCheck()
	}
	return o.runTest()
}

func (o *InterpretOptions) runCheck() error {
	obj, err := o.CustomizationResult.Object()
	if err != nil {
		return err
	}

	customization := &configv1alpha1.ResourceInterpreterCustomization{}
	err = helper.ConvertToTypedObject(obj, customization)
	if err != nil {
		return err
	}

	kind := customization.Spec.Target.Kind
	if kind == "" {
		return fmt.Errorf("target.kind no set")
	}
	apiVersion := customization.Spec.Target.APIVersion
	if apiVersion == "" {
		return fmt.Errorf("target.apiVersion no set")
	}

	w := printers.GetNewTabWriter(o.Out)
	defer w.Flush()
	fmt.Fprintf(w, "TARGET:%s %s\t\n", apiVersion, kind)
	fmt.Fprintf(w, "RULERS:\n")
	for _, r := range o.Rules {
		fmt.Fprintf(w, "    %s:\t", r.name)

		script := r.scriptGetter(customization)
		if script == "" {
			fmt.Fprintln(w, "DISABLED")
		} else {
			err = checkScrip(script)
			if err != nil {
				fmt.Fprintf(w, "%s: %v\t\n", "ERROR", err)
			} else {
				fmt.Fprintln(w, "PASS")
			}
		}
	}
	return nil
}

func (o *InterpretOptions) runTest() error {
	if o.Operation == "" {
		return fmt.Errorf("operation is not set for testing")
	}

	c, err := o.getCustomizationObject()
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

	var interpreter resourceinterpreter.ResourceInterpreter

	for _, r := range o.Rules {
		if r.name == o.Operation {
			script := r.scriptGetter(c)
			results, err := r.run(interpreter, script, args)
			printResult(o.Out, o.ErrOut, results, err)
		}
	}
	return fmt.Errorf("operation %s is not supported. Use one of: %s", o.Operation, strings.Join(o.Rules.Names(), ", "))
}

func printResult(w, errOut io.Writer, results *ruleResults, err error) {
	if err != nil {
		fmt.Fprintf(errOut, "ERROR: %v\n", err)
		return
	}

	encoder := yaml.NewEncoder(w)
	for _, result := range *results {
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w, "# "+result.name)
		if err = encoder.Encode(result.value); err != nil {
			fmt.Fprintf(errOut, "ERROR: %v\n", err)
		}
	}
}

func (o *InterpretOptions) getCustomizationObject() (*configv1alpha1.ResourceInterpreterCustomization, error) {
	infos, err := o.CustomizationResult.Infos()
	if err != nil {
		return nil, err
	}
	switch len(infos) {
	case 0:
		return nil, fmt.Errorf("no customization is specified by args or filename option")
	case 1:
		c := &configv1alpha1.ResourceInterpreterCustomization{}
		err = helper.ConvertToTypedObject(infos[0].Object, c)
		return c, err
	default:
		return nil, fmt.Errorf("you can only specify one customization by args and filename option")
	}
}

func (o *InterpretOptions) getDesiredObject() (*unstructured.Unstructured, error) {
	var obj *unstructured.Unstructured
	err := o.DesiredResult.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if obj != nil {
			return fmt.Errorf("you can not specify more desired object by desired and desired-file option")
		}
		obj, err = helper.ToUnstructured(info.Object)
		return err
	})
	return obj, err
}

func (o *InterpretOptions) getObservedObject() (*unstructured.Unstructured, error) {
	var obj *unstructured.Unstructured
	err := o.ObservedResult.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if obj != nil {
			return fmt.Errorf("you can not specify more observed object by observed and observed-file option")
		}
		obj, err = helper.ToUnstructured(info.Object)
		return err
	})
	return obj, err
}

func (o *InterpretOptions) getAggregatedStatusItems() ([]workv1alpha2.AggregatedStatusItem, error) {
	objs, err := o.StatusResult.Objects()
	if err != nil {
		return nil, err
	}
	items := make([]workv1alpha2.AggregatedStatusItem, len(objs))
	for i, obj := range objs {
		items[i] = obj.(workv1alpha2.AggregatedStatusItem)
	}
	return items, nil
}

type ruleArgs struct {
	Desired  *unstructured.Unstructured
	Observed *unstructured.Unstructured
	Status   []workv1alpha2.AggregatedStatusItem
	Replica  int64
}

func (r ruleArgs) getDesiredObjectOrError() (*unstructured.Unstructured, error) {
	if r.Desired == nil {
		return nil, fmt.Errorf("desired, desired-file options are not set")
	}
	return r.Desired, nil
}

func (r ruleArgs) getObservedObjectOrError() (*unstructured.Unstructured, error) {
	if r.Observed == nil {
		return nil, fmt.Errorf("observed, observed-file options are not set")
	}
	return r.Observed, nil
}

func (r ruleArgs) getObjectOrError() (*unstructured.Unstructured, error) {
	if r.Desired == nil || r.Observed == nil {
		return nil, fmt.Errorf("desired, desired-file, observed, observed-file options are not set")
	}
	if r.Desired != nil || r.Observed != nil {
		return nil, fmt.Errorf("you can not specify multiple object by desired, desired-file, observed, observed-file options")
	}
	if r.Desired != nil {
		return r.Desired, nil
	}
	return r.Observed, nil
}

type rule struct {
	name         string
	scriptGetter func(*configv1alpha1.ResourceInterpreterCustomization) string
	run          func(interpret resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error)
}

type rules []rule

func (r rules) Names() []string {
	names := make([]string, len(r))
	for i, rr := range r {
		names[i] = rr.name
	}
	return names
}

type ruleResult struct {
	name  string
	value interface{}
}

type ruleResults []ruleResult

func newRuleResults() *ruleResults {
	return &ruleResults{}
}

func (r *ruleResults) add(name string, value interface{}) *ruleResults {
	*r = append(*r, ruleResult{name: name, value: value})
	return r
}

var defaultRules rules = []rule{
	{
		name: interpretOperationRetention,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.Retention != nil {
				return c.Spec.Customizations.Retention.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			desired, err := args.getDesiredObjectOrError()
			if err != nil {
				return nil, err
			}
			observed, err := args.getObservedObjectOrError()
			if err != nil {
				return nil, err
			}
			retained, err := interpreter.Retain(desired, observed)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("retained", retained), nil
		},
	},
	{
		name: interpretOperationReplicaResource,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.ReplicaResource != nil {
				return c.Spec.Customizations.ReplicaResource.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			replica, requires, err := interpreter.GetReplicas(obj)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("replica", replica).add("requires", requires), nil
		},
	},
	{
		name: interpretOperationReplicaRevision,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.ReplicaRevision != nil {
				return c.Spec.Customizations.ReplicaRevision.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			revised, err := interpreter.ReviseReplica(obj, args.Replica)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("revised", revised), nil
		},
	},
	{
		name: interpretOperationStatusReflection,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.StatusReflection != nil {
				return c.Spec.Customizations.StatusReflection.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			status, err := interpreter.ReflectStatus(obj)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("status", status), nil
		},
	},
	{
		name: interpretOperationStatusAggregation,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.StatusAggregation != nil {
				return c.Spec.Customizations.StatusAggregation.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			aggregateStatus, err := interpreter.AggregateStatus(obj, args.Status)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("aggregateStatus", aggregateStatus), nil
		},
	},
	{
		name: interpretOperationHealthInterpretation,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.HealthInterpretation != nil {
				return c.Spec.Customizations.HealthInterpretation.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			healthy, err := interpreter.InterpretHealth(obj)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("healthy", healthy), nil
		},
	},
	{
		name: interpretOperationDependencyInterpretation,
		scriptGetter: func(c *configv1alpha1.ResourceInterpreterCustomization) string {
			if c.Spec.Customizations.DependencyInterpretation != nil {
				return c.Spec.Customizations.DependencyInterpretation.LuaScript
			}
			return ""
		},
		run: func(interpreter resourceinterpreter.ResourceInterpreter, script string, args ruleArgs) (*ruleResults, error) {
			obj, err := args.getObjectOrError()
			if err != nil {
				return nil, err
			}
			dependencies, err := interpreter.GetDependencies(obj)
			if err != nil {
				return nil, err
			}
			return newRuleResults().add("dependencies", dependencies), nil
		},
	},
}

func checkScrip(script string) error {
	// TODO: check script with lua VM.
	return nil
}

const (
	interpretOperationRetention                = "retention"
	interpretOperationReplicaResource          = "replicaResource"
	interpretOperationReplicaRevision          = "replicaRevision"
	interpretOperationStatusReflection         = "statusReflection"
	interpretOperationStatusAggregation        = "statusAggregation"
	interpretOperationHealthInterpretation     = "healthInterpretation"
	interpretOperationDependencyInterpretation = "dependencyInterpretation"

	customizationResourceType = "resourceinterpretercustomizations"
)
