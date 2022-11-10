package karmadactl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog/v2"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/cmd/util/editor"
	"k8s.io/kubectl/pkg/cmd/util/editor/crlf"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

func (o *InterpretOptions) runEdit() error {
	infos, err := o.EditOptions.OriginalResult.Infos()
	if err != nil {
		return err
	}
	if len(infos) != 1 {
		return fmt.Errorf("only one customization can be edited")
	}

	edit := editor.NewDefaultEditor(editorEnvs())

	// return o.EditOptions.Run()
	editFn := func(info *resource.Info) error {
		var (
			results = editResults{}
			edited  = []byte{}
			file    string
			err     error
		)

		containsError := false
		// loop until we succeed or cancel editing
		for {

			originalCustomization := &configv1alpha1.ResourceInterpreterCustomization{}
			err = helper.ConvertToTypedObject(info.Object, originalCustomization)
			if err != nil {
				return err
			}

			// generate the file to edit
			buf := &bytes.Buffer{}
			var w io.Writer = buf
			if o.WindowsLineEndings {
				w = crlf.NewCRLFWriter(w)
			}

			err = results.header.writeTo(w, o.EditMode)
			if err != nil {
				return err
			}

			if !containsError {
				printCustomization(w, originalCustomization, o.Rules)
			} else {
				// In case of an error, preserve the edited file.
				// Remove the comments (header) from it since we already
				// have included the latest header in the buffer above.
				buf.Write(stripComments(edited))
			}

			// launch the editor
			editedDiff := edited
			edited, file, err = edit.LaunchTempFile(fmt.Sprintf("%s-edit-", filepath.Base(os.Args[0])), "lua", buf)
			if err != nil {
				return preservedFile(err, results.file, o.ErrOut)
			}

			// If we're retrying the loop because of an error, and no change was made in the file, short-circuit
			if containsError && bytes.Equal(stripComments(editedDiff), stripComments(edited)) {
				return preservedFile(fmt.Errorf("%s", "Edit cancelled, no valid changes were saved."), file, o.ErrOut)
			}
			// cleanup any file from the previous pass
			if len(results.file) > 0 {
				os.Remove(results.file)
			}
			klog.V(4).Infof("User edited:\n%s", string(edited))

			// build new customization from edited file
			editedSpec, err := buildCustomizationSpecFromEdited(edited, o.Rules)
			if err != nil {
				results = editResults{
					file: file,
				}
				containsError = true
				fmt.Fprintln(o.ErrOut, results.addError(apierrors.NewInvalid(corev1.SchemeGroupVersion.WithKind("").GroupKind(),
					"", field.ErrorList{field.Invalid(nil, "The edited file failed validation", fmt.Sprintf("%v", err))}), infos[0]))
				continue
			}

			// Compare content
			if reflect.DeepEqual(originalCustomization.Spec, editedSpec) {
				os.Remove(file)
				fmt.Fprintln(o.ErrOut, "Edit cancelled, no changes made.")
				return nil
			}

			lines, err := hasLines(bytes.NewBuffer(edited))
			if err != nil {
				return preservedFile(err, file, o.ErrOut)
			}
			if !lines {
				os.Remove(file)
				fmt.Fprintln(o.ErrOut, "Edit cancelled, saved file was empty.")
				return nil
			}

			// not a syntax error as it turns out...
			editedCustomization := &configv1alpha1.ResourceInterpreterCustomization{}
			originalCustomization.DeepCopyInto(editedCustomization)
			editedCustomization.Spec = editedSpec

			// TODO: validate edited customization

			containsError = false
			results = editResults{
				file: file,
			}

			updatedVisitor := resource.InfoListVisitor{
				{
					Client:          info.Client,
					Mapping:         info.Mapping,
					Namespace:       info.Namespace,
					Name:            info.Name,
					Source:          info.Source,
					Object:          editedCustomization,
					ResourceVersion: editedCustomization.ResourceVersion,
					Subresource:     info.Subresource,
				},
			}

			// iterate through all items to apply annotations
			if err := visitAnnotation(o.EditOptions, updatedVisitor); err != nil {
				return preservedFile(err, file, o.ErrOut)
			}

			switch {
			case info.Source != "":
				err = o.saveToPath(info, editedCustomization, &results)
			default:
				err = o.saveToServer(info, editedCustomization, &results)
			}
			if err != nil {
				return preservedFile(err, results.file, o.ErrOut)
			}

			// Handle all possible errors
			//
			// 1. retryable: propose kubectl replace -f
			// 2. notfound: indicate the location of the saved configuration of the deleted resource
			// 3. invalid: retry those on the spot by looping ie. reloading the editor
			if results.retryable > 0 {
				fmt.Fprintf(o.ErrOut, "You can run `%s replace -f %s` to try this update again.\n", filepath.Base(os.Args[0]), file)
				return cmdutil.ErrExit
			}
			if results.notfound > 0 {
				fmt.Fprintf(o.ErrOut, "The edits you made on deleted resources have been saved to %q\n", file)
				return cmdutil.ErrExit
			}

			if len(results.edit) == 0 {
				if results.notfound == 0 {
					os.Remove(file)
				} else {
					fmt.Fprintf(o.Out, "The edits you made on deleted resources have been saved to %q\n", file)
				}
				return nil
			}

			if len(results.header.reasons) > 0 {
				containsError = true
			}
		}
	}
	return editFn(infos[0])
}

func (o *InterpretOptions) saveToPath(originalInfo *resource.Info, editedObj runtime.Object, results *editResults) error {
	var w io.Writer
	var writer printers.ResourcePrinter = &printers.YAMLPrinter{}
	source := originalInfo.Source
	switch {
	case source == "":
		return fmt.Errorf("resource %s/%s is not from file", originalInfo.Namespace, originalInfo.Name)
	case source == "STDIN" || strings.HasPrefix(source, "http"):
		w = os.Stdout
	default:
		f, err := os.OpenFile(originalInfo.Source, os.O_RDWR|os.O_TRUNC, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f

		_, _, isJson := yaml.GuessJSONStream(f, 4096)
		if isJson {
			writer = &printers.JSONPrinter{}
		}
	}

	err := writer.PrintObj(editedObj, w)
	if err != nil {
		fmt.Fprintf(o.ErrOut, results.addError(err, originalInfo))
	}

	printer, err := o.ToPrinter("edited")
	if err != nil {
		return err
	}
	return printer.PrintObj(originalInfo.Object, o.Out)
}

func (o *InterpretOptions) saveToServer(originalInfo *resource.Info, editedObj runtime.Object, results *editResults) error {
	originalJS, err := json.Marshal(originalInfo.Object)
	if err != nil {
		return err
	}

	editedJS, err := json.Marshal(editedObj)
	if err != nil {
		return err
	}

	preconditions := []mergepatch.PreconditionFunc{
		mergepatch.RequireKeyUnchanged("apiVersion"),
		mergepatch.RequireKeyUnchanged("kind"),
		mergepatch.RequireMetadataKeyUnchanged("name"),
		mergepatch.RequireKeyUnchanged("managedFields"),
	}

	patchType := types.MergePatchType
	patch, err := jsonpatch.CreateMergePatch(originalJS, editedJS)
	if err != nil {
		klog.V(4).Infof("Unable to calculate diff, no merge is possible: %v", err)
		return err
	}
	var patchMap map[string]interface{}
	err = json.Unmarshal(patch, &patchMap)
	if err != nil {
		klog.V(4).Infof("Unable to calculate diff, no merge is possible: %v", err)
		return err
	}
	for _, precondition := range preconditions {
		if !precondition(patchMap) {
			klog.V(4).Infof("Unable to calculate diff, no merge is possible: %v", err)
			return fmt.Errorf("%s", "At least one of apiVersion, kind and name was changed")
		}
	}

	if o.OutputPatch {
		fmt.Fprintf(o.Out, "Patch: %s\n", string(patch))
	}

	patched, err := resource.NewHelper(originalInfo.Client, originalInfo.Mapping).
		WithFieldManager(o.FieldManager).
		WithFieldValidation(o.ValidationDirective).
		WithSubresource(o.Subresource).
		Patch(originalInfo.Namespace, originalInfo.Name, patchType, patch, nil)
	if err != nil {
		fmt.Fprintln(o.ErrOut, results.addError(err, originalInfo))
		return nil
	}
	originalInfo.Refresh(patched, true)
	printer, err := o.ToPrinter("edited")
	if err != nil {
		return err
	}
	return printer.PrintObj(originalInfo.Object, o.Out)
}

const (
	luaCommentPrefix    = "--"
	luaAnnotationPrefix = "---@"
	luaAnnotationTarget = luaAnnotationPrefix + "TARGET:"
	luaAnnotationRule   = luaAnnotationPrefix + "RULE:"
)

func printCustomization(w io.Writer, c *configv1alpha1.ResourceInterpreterCustomization, rules Rules) {
	fmt.Fprintf(w, "%s %s %s\n", luaAnnotationTarget, c.Spec.Target.APIVersion, c.Spec.Target.Kind)
	for _, r := range rules {
		fmt.Fprintf(w, "%s %s\n", luaAnnotationRule, r.Name())
		fmt.Fprintln(w, r.GetScript(c))
	}
}

// Buf is like:
// -- Please edit the object below. Lines beginning with a '--' will be ignored,
// ---@TARGET: apps/v1 Deployment
// ---@RULE: retention
// function Retain()
//    ...
// end
// ---@RULE: replicaResource
// ---@RULE:replicaRevision
func buildCustomizationSpecFromEdited(file []byte, rules Rules) (configv1alpha1.ResourceInterpreterCustomizationSpec, error) {
	customization := &configv1alpha1.ResourceInterpreterCustomization{}
	var currRule Rule
	var script string
	lines := bytes.Split(file, []byte("\n"))
	for _, line := range lines {
		trimline := bytes.TrimSpace(line)

		switch {
		case bytes.HasPrefix(trimline, []byte(luaAnnotationTarget)):
			s := bytes.TrimSpace(bytes.TrimPrefix(trimline, []byte(luaAnnotationTarget)))
			// get parts: [apps/v1, Deployment]
			parts := bytes.SplitN(s, []byte(" "), 2)
			customization.Spec.Target.APIVersion = string(parts[0])
			if len(parts) > 1 {
				customization.Spec.Target.Kind = string(bytes.TrimSpace(parts[1]))
			}
		case bytes.HasPrefix(trimline, []byte(luaAnnotationRule)):
			name := string(bytes.TrimSpace(bytes.TrimPrefix(trimline, []byte(luaAnnotationRule))))
			r := rules.Get(name)
			if r != nil {
				if currRule != nil {
					currRule.SetScript(customization, script)
				}
				currRule = r
				script = ""
			}
		case bytes.HasPrefix(trimline, []byte(luaCommentPrefix)):
			// comments are skipped
		default:
			if currRule == nil {
				return customization.Spec, fmt.Errorf("bad format")
			} else {
				if len(script) != 0 {
					script += "\n"
				}
				script += string(line)
			}
		}
	}

	if currRule != nil {
		currRule.SetScript(customization, script)
	}
	return customization.Spec, nil
}

func stripComments(file []byte) []byte {
	stripped := make([]byte, 0, len(file))

	lines := bytes.Split(file, []byte("\n"))
	for _, line := range lines {
		trimline := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimline, []byte(luaCommentPrefix)) && !bytes.HasPrefix(trimline, []byte(luaAnnotationPrefix)) {
			continue
		}
		if len(stripped) != 0 {
			stripped = append(stripped, '\n')
		}
		stripped = append(stripped, line...)
	}
	return stripped
}

// ---------------------------- lifted ----------------------------

func visitAnnotation(o *editor.EditOptions, annotationVisitor resource.Visitor) error {
	// iterate through all items to apply annotations
	err := annotationVisitor.Visit(func(info *resource.Info, incomingErr error) error {
		// put configuration annotation in "updates"
		if o.ApplyAnnotation {
			if err := util.CreateOrUpdateAnnotation(true, info.Object, scheme.DefaultJSONEncoder()); err != nil {
				return err
			}
		}
		if err := o.Recorder.Record(info.Object); err != nil {
			klog.V(4).Infof("error recording current command: %v", err)
		}

		return nil

	})
	return err
}

// editReason preserves a message about the reason this file must be edited again
type editReason struct {
	head  string
	other []string
}

// editHeader includes a list of reasons the edit must be retried
type editHeader struct {
	reasons []editReason
}

// writeTo outputs the current header information into a stream
func (h *editHeader) writeTo(w io.Writer, editMode editor.EditMode) error {
	if editMode == editor.ApplyEditMode {
		fmt.Fprint(w, `-- Please edit the 'last-applied-configuration' annotations below.
-- Lines beginning with a '--' will be ignored, and an empty file will abort the edit.
--
`)
	} else {
		fmt.Fprint(w, `-- Please edit the object below. Lines beginning with a '--' will be ignored,
-- and an empty file will abort the edit. If an error occurs while saving this file will be
-- reopened with the relevant failures.
--
`)
	}

	for _, r := range h.reasons {
		if len(r.other) > 0 {
			fmt.Fprintf(w, "# %s:\n", hashOnLineBreak(r.head))
		} else {
			fmt.Fprintf(w, "# %s\n", hashOnLineBreak(r.head))
		}
		for _, o := range r.other {
			fmt.Fprintf(w, "# * %s\n", hashOnLineBreak(o))
		}
		fmt.Fprintln(w, "#")
	}
	return nil
}

// editResults capture the result of an update
type editResults struct {
	header    editHeader
	retryable int
	notfound  int
	edit      []*resource.Info
	file      string
}

func (r *editResults) addError(err error, info *resource.Info) string {
	resourceString := info.Mapping.Resource.Resource
	if len(info.Mapping.Resource.Group) > 0 {
		resourceString = resourceString + "." + info.Mapping.Resource.Group
	}

	switch {
	case apierrors.IsInvalid(err):
		r.edit = append(r.edit, info)
		reason := editReason{
			head: fmt.Sprintf("%s %q was not valid", resourceString, info.Name),
		}
		if err, ok := err.(apierrors.APIStatus); ok {
			if details := err.Status().Details; details != nil {
				for _, cause := range details.Causes {
					reason.other = append(reason.other, fmt.Sprintf("%s: %s", cause.Field, cause.Message))
				}
			}
		}
		r.header.reasons = append(r.header.reasons, reason)
		return fmt.Sprintf("error: %s %q is invalid", resourceString, info.Name)
	case apierrors.IsNotFound(err):
		r.notfound++
		return fmt.Sprintf("error: %s %q could not be found on the server", resourceString, info.Name)
	default:
		r.retryable++
		return fmt.Sprintf("error: %s %q could not be patched: %v", resourceString, info.Name, err)
	}
}

// hashOnLineBreak returns a string built from the provided string by inserting any necessary '#'
// characters after '\n' characters, indicating a comment.
func hashOnLineBreak(s string) string {
	r := ""
	for i, ch := range s {
		j := i + 1
		if j < len(s) && ch == '\n' && s[j] != '#' {
			r += "\n# "
		} else {
			r += string(ch)
		}
	}
	return r
}

// editorEnvs returns an ordered list of env vars to check for editor preferences.
func editorEnvs() []string {
	return []string{
		"KUBE_EDITOR",
		"EDITOR",
	}
}

// preservedFile writes out a message about the provided file if it exists to the
// provided output stream when an error happens. Used to notify the user where
// their updates were preserved.
func preservedFile(err error, path string, out io.Writer) error {
	if len(path) > 0 {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			fmt.Fprintf(out, "A copy of your changes has been stored to %q\n", path)
		}
	}
	return err
}

// hasLines returns true if any line in the provided stream is non empty - has non-whitespace
// characters, or the first non-whitespace character is a '#' indicating a comment. Returns
// any errors encountered reading the stream.
func hasLines(r io.Reader) (bool, error) {
	// TODO: if any files we read have > 64KB lines, we'll need to switch to bytes.ReadLine
	// TODO: probably going to be secrets
	s := bufio.NewScanner(r)
	for s.Scan() {
		if line := strings.TrimSpace(s.Text()); len(line) > 0 && line[0] != '#' {
			return true, nil
		}
	}
	if err := s.Err(); err != nil && err != io.EOF {
		return false, err
	}
	return false, nil
}
