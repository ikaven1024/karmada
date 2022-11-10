package karmadactl

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
)

func (o *InterpretOptions) runCheck() error {
	w := printers.GetNewTabWriter(o.Out)
	defer w.Flush()

	failed := false

	err := o.CustomizationResult.Visit(func(info *resource.Info, err error) error {
		fmt.Fprintln(w, "-----------------------------------")

		source := info.Source
		if info.Name != "" {
			source = info.Name
		}
		fmt.Fprintf(w, "SOURCE: %s\n", source)

		customization, err := asResourceInterpreterCustomization(info.Object)
		if err != nil {
			return err
		}

		kind := customization.Spec.Target.Kind
		if kind == "" {
			failed = true
			fmt.Fprintln(w, "target.kind no set")
			return nil
		}
		apiVersion := customization.Spec.Target.APIVersion
		if apiVersion == "" {
			failed = true
			fmt.Fprintln(w, "target.apiVersion no set")
			return nil
		}

		fmt.Fprintf(w, "TARGET: %s %s\t\n", apiVersion, kind)
		fmt.Fprintf(w, "RULERS:\n")
		for _, r := range o.Rules {
			fmt.Fprintf(w, "    %s:\t", r.Name())

			script := r.GetScript(customization)
			if script == "" {
				fmt.Fprintln(w, "DISABLED")
			} else {
				err = checkScrip(script)
				if err != nil {
					failed = true
					fmt.Fprintf(w, "%s: %v\t\n", "ERROR", err)
				} else {
					fmt.Fprintln(w, "PASS")
				}
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	if failed {
		// As failed infos are printed above. So don't print it again.
		return fmt.Errorf("")
	}
	return nil
}

func checkScrip(script string) error {
	l := lua.NewState()
	defer l.Close()
	_, err := l.LoadString(script)
	return err
}
