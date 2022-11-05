package karmadactl

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/karmada-io/karmada/pkg/karmadactl/util"
)

var (
	interpretLong = templates.LongDesc(`
		Check the valid of resource interpret customizations. Or show the result of the rules running.


		1. Check valid. Given the customization by NAME or '-f' option, this tool will
		   check the rules in customization, and print 'PASS' or 'FAIL' for each rule.
		   The fail reasons are also printed.


		2. Show result. Given the customization by NAME or '-f' option, the operation by
		   'operation' option, and arguments by options(desired, desired-file, observed,
		   observed-file, status-file, desired-replica),  this tool will run the
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
	o := &InterpretOptions{}
	cmd := &cobra.Command{
		Use:                   "interpreter (-f FILENAME | NAME) (--operation OPERATION) [-n NAMESPACE] [--ARGS VALUE]... ",
		Short:                 "Check the valid of customizations. Or show the result of the rules running",
		Long:                  interpretLong,
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		Example:               fmt.Sprintf(interpretExample, parentCommand),
		RunE: func(cmd *cobra.Command, args []string) error {
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
	flags.StringVar(&o.Operation, "operation", o.Operation, "The interpret operation to use. One of: ("+operationListString()+")")
	flags.StringVarP(&o.Filename, "filename", "f", o.Filename, "Filename, directory, or URL to files identifying the resource to fetch the customization rules.")
	flags.BoolVar(&o.Check, "check", o.Check, "Check the given customizations")
	flags.StringVar(&o.Desired, "desired", o.Desired, "The type and name of resource on server to use as desiredObj argument in rule script. For example: deployment/foo.")
	flags.StringVar(&o.DesiredFile, "desired-file", o.DesiredFile, "Filename, directory, or URL to files identifying the resource to use as desiredObj argument in rule script.")
	flags.StringVar(&o.Observed, "observed", o.Observed, "The type and name of resource on server to use as observedObj argument in rule script. For example: deployment/foo.")
	flags.StringVar(&o.ObservedFile, "observed-file", o.ObservedFile, "Filename, directory, or URL to files identifying the resource to use as observedObj argument in rule script.")
	flags.StringSliceVar(&o.StatusFile, "status-file", o.StatusFile, "Filename, directory, or URL to files identifying the resource to use as statusItems argument in rule script.")
	flags.Int32Var(&o.DesiredReplica, "desired-replica", o.DesiredReplica, "The desiredReplica argument in rule script.")
	flags.StringVarP(&o.Namespace, "namespace", "n", o.Namespace, "If present, the namespace scope for desired or observed object from server.")

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
	Namespace      string
}

// Complete ensures that options are valid and marshals them if necessary
func (o *InterpretOptions) Complete(f util.Factory, cmd *cobra.Command, args []string) error {
	return nil
}

// Run describe information of resources
func (o *InterpretOptions) Run() error {
	return fmt.Errorf("not implement")
}

const (
	interpretOperationRetention                = "retention"
	interpretOperationReplicaResource          = "replicaResource"
	interpretOperationReplicaRevision          = "replicaRevision"
	interpretOperationStatusReflection         = "statusReflection"
	interpretOperationStatusAggregation        = "statusAggregation"
	interpretOperationHealthInterpretation     = "healthInterpretation"
	interpretOperationDependencyInterpretation = "dependencyInterpretation"
)

func operationListString() string {
	operations := []string{
		interpretOperationRetention,
		interpretOperationReplicaResource,
		interpretOperationReplicaRevision,
		interpretOperationStatusReflection,
		interpretOperationStatusAggregation,
		interpretOperationHealthInterpretation,
		interpretOperationDependencyInterpretation,
	}
	return strings.Join(operations, ", ")
}
