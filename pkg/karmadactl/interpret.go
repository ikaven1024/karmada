package karmadactl

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/util/editor"
	"k8s.io/kubectl/pkg/util/templates"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/karmadactl/util"
	"github.com/karmada-io/karmada/pkg/karmadactl/util/genericresource"
	"github.com/karmada-io/karmada/pkg/util/gclient"
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
		EditOptions: editor.NewEditOptions(editor.NormalEditMode, streams),
		IOStreams:   streams,
		Rules:       allRules,
	}
	cmd := &cobra.Command{
		Use:                   "interpret (-f FILENAME | NAME) (--operation OPERATION) [-n NAMESPACE] [--ARGS VALUE]... ",
		Short:                 "Check the valid of customizations. Or show the result of the rules running",
		Long:                  interpretLong,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		Example:               fmt.Sprintf(interpretExample, parentCommand),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
		Annotations: map[string]string{
			util.TagCommandGroup: util.GroupClusterTroubleshootingAndDebugging,
		},
	}

	flags := cmd.Flags()
	o.EditOptions.RecordFlags.AddFlags(cmd)
	o.EditOptions.PrintFlags.AddFlags(cmd)
	cmdutil.AddValidateFlags(cmd)
	flags.StringVar(&o.Operation, "operation", o.Operation, "The interpret operation to use. One of: ("+strings.Join(o.Rules.Names(), ",")+")")
	flags.BoolVar(&o.Check, "check", o.Check, "Check the given customizations")
	flags.BoolVar(&o.Edit, "edit", o.Edit, "Edit customizations")
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

	cmdutil.AddFilenameOptionFlags(cmd, &o.FilenameOptions, "containing the customizations")
	_ = flags.MarkHidden("kustomize")

	return cmd
}

// InterpretOptions contains the input to the interpret command.
type InterpretOptions struct {
	resource.FilenameOptions
	*editor.EditOptions

	Operation string
	Filename  string
	Check     bool
	Edit      bool

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

	updatedResultGetter func(data []byte) *resource.Result

	Rules Rules

	genericclioptions.IOStreams
}

// Complete ensures that options are valid and marshals them if necessary
func (o *InterpretOptions) Complete(f util.Factory, cmd *cobra.Command, args []string) error {
	cmdNamespace, enforceNamespace, err := f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	if o.Edit {
		if len(args) > 1 {
			return fmt.Errorf("you can only edit one customization at once")
		}
		if len(args) == 1 {
			args[0] = "resourceinterpretercustomizations/" + args[0]
		}
		o.EditOptions.FilenameOptions = o.FilenameOptions
		return o.EditOptions.Complete(f, args, cmd)
	}

	o.CustomizationResult = f.NewBuilder().
		WithScheme(gclient.NewSchema(), gclient.NewSchema().PrioritizedVersionsAllGroups()...).
		NamespaceParam(cmdNamespace).
		DefaultNamespace().
		FilenameParam(enforceNamespace, &o.FilenameOptions).
		ResourceNames("resourceinterpretercustomizations", args...).
		RequireObject(true).
		Do()
	if err = o.CustomizationResult.Err(); err != nil {
		return err
	}

	if o.Desired != "" || o.DesiredFile != "" {
		o.DesiredResult = f.NewBuilder().
			Unstructured().
			NamespaceParam(cmdNamespace).
			DefaultNamespace().
			FilenameParam(enforceNamespace, &resource.FilenameOptions{Filenames: filterOutEmptyString(o.DesiredFile)}).
			ResourceTypeOrNameArgs(false, filterOutEmptyString(o.Desired)...).
			RequireObject(true).
			Do()
		if err = o.DesiredResult.Err(); err != nil {
			return err
		}
	}

	if o.Observed != "" || o.ObservedFile != "" {
		o.ObservedResult = f.NewBuilder().
			Unstructured().
			NamespaceParam(cmdNamespace).
			DefaultNamespace().
			FilenameParam(enforceNamespace, &resource.FilenameOptions{Filenames: filterOutEmptyString(o.ObservedFile)}).
			ResourceTypeOrNameArgs(false, filterOutEmptyString(o.Observed)...).
			RequireObject(true).
			Do()
		if err = o.ObservedResult.Err(); err != nil {
			return err
		}
	}

	if len(o.StatusFile) > 0 {
		o.StatusResult = genericresource.NewBuilder().
			Constructor(func() interface{} { return &workv1alpha2.AggregatedStatusItem{} }).
			Filename(true, o.StatusFile...).
			Do()
		if err = o.StatusResult.Err(); err != nil {
			return err
		}
	}

	return nil
}

// Validate checks the EditOptions to see if there is sufficient information to run the command.
func (o *InterpretOptions) Validate() error {
	if o.Edit {
		return o.EditOptions.Validate()
	}
	return nil
}

// Run describe information of resources
func (o *InterpretOptions) Run() error {
	switch {
	case o.Check:
		return o.runCheck()
	case o.Edit:
		return o.runEdit()
	default:
		return o.runTest()
	}
}

func (o *InterpretOptions) getCustomizationObject() ([]*configv1alpha1.ResourceInterpreterCustomization, error) {
	infos, err := o.CustomizationResult.Infos()
	if err != nil {
		return nil, err
	}

	customizations := make([]*configv1alpha1.ResourceInterpreterCustomization, len(infos))
	for i, info := range infos {
		c, err := asResourceInterpreterCustomization(info.Object)
		if err != nil {
			return nil, err
		}
		customizations[i] = c
	}
	return customizations, nil
}

func (o *InterpretOptions) getDesiredObject() (*unstructured.Unstructured, error) {
	if o.DesiredResult == nil {
		return nil, nil
	}

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
	if o.ObservedResult == nil {
		return nil, nil
	}

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
	if o.StatusResult == nil {
		return nil, nil
	}

	objs, err := o.StatusResult.Objects()
	if err != nil {
		return nil, err
	}
	items := make([]workv1alpha2.AggregatedStatusItem, len(objs))
	for i, obj := range objs {
		items[i] = *(obj.(*workv1alpha2.AggregatedStatusItem))
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
	if r.Desired == nil && r.Observed == nil {
		return nil, fmt.Errorf("desired, desired-file, observed, observed-file options are not set")
	}
	if r.Desired != nil && r.Observed != nil {
		return nil, fmt.Errorf("you can not specify multiple object by desired, desired-file, observed, observed-file options")
	}
	if r.Desired != nil {
		return r.Desired, nil
	}
	return r.Observed, nil
}

func filterOutEmptyString(ss ...string) []string {
	ret := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != "" {
			ret = append(ret, s)
		}
	}
	return ret
}

func asResourceInterpreterCustomization(o runtime.Object) (*configv1alpha1.ResourceInterpreterCustomization, error) {
	c, ok := o.(*configv1alpha1.ResourceInterpreterCustomization)
	if !ok {
		return nil, fmt.Errorf("not a ResourceInterpreterCustomization: %#v", o)
	}
	return c, nil
}
