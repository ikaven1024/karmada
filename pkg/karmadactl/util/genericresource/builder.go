package genericresource

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/resource"
)

const defaultHttpGetAttempts int = 3

// Builder provides convenience functions for taking arguments and parameters
// from the command line and converting them to a list of resources to iterate
// over using the Visitor interface.
type Builder struct {
	errs            []error
	paths           []Visitor
	continueOnError bool
	stdinInUse      bool
	mapper          *mapper
	schema          resource.ContentValidator
}

// NewBuilder returns a builder that is configured not to create REST clients and avoids asking the server for results.
func NewBuilder() *Builder {
	return &Builder{
		mapper: &mapper{
			newFunc: func() interface{} {
				return map[string]interface{}{}
			},
		},
	}
}

func (b *Builder) Constructor(newFunc func() interface{}) *Builder {
	b.mapper.newFunc = newFunc
	return b
}

// ContinueOnError will attempt to load and visit as many objects as possible, even if some visits
// return errors or some objects cannot be loaded. The default behavior is to terminate after
// the first error is returned from a VisitorFunc.
func (b *Builder) ContinueOnError() *Builder {
	b.continueOnError = true
	return b
}

func (b *Builder) Filename(recursive bool, filenames ...string) *Builder {
	for _, s := range filenames {
		switch {
		case s == "-":
			b.Stdin()
		case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
			u, err := url.Parse(s)
			if err != nil {
				b.errs = append(b.errs, fmt.Errorf("the URL passed to filename %q is not valid: %v", s, err))
				continue
			}
			b.URL(defaultHttpGetAttempts, u)
		default:
			matches, err := expandIfFilePattern(s)
			if err != nil {
				b.errs = append(b.errs, err)
				continue
			}
			b.Path(recursive, matches...)
		}
	}
	return b
}

// Stdin will read objects from the standard input. If ContinueOnError() is set
// prior to this method being called, objects in the stream that are unrecognized
// will be ignored (but logged at V(2)). If StdinInUse() is set prior to this method
// being called, an error will be recorded as there are multiple entities trying to use
// the single standard input stream.
func (b *Builder) Stdin() *Builder {
	if b.stdinInUse {
		b.errs = append(b.errs, resource.StdinMultiUseError)
	}
	b.stdinInUse = true
	b.paths = append(b.paths, FileVisitorForSTDIN(b.mapper, b.schema))
	return b
}

// URL accepts a number of URLs directly.
func (b *Builder) URL(httpAttemptCount int, urls ...*url.URL) *Builder {
	for _, u := range urls {
		b.paths = append(b.paths, NewURLVisitor(b.mapper, httpAttemptCount, u, b.schema))
	}
	return b
}

// Path accepts a set of paths that may be files, directories (all can containing
// one or more resources). Creates a FileVisitor for each file and then each
// FileVisitor is streaming the content to a StreamVisitor. If ContinueOnError() is set
// prior to this method being called, objects on the path that are unrecognized will be
// ignored (but logged at V(2)).
func (b *Builder) Path(recursive bool, paths ...string) *Builder {
	for _, p := range paths {
		_, err := os.Stat(p)
		if os.IsNotExist(err) {
			b.errs = append(b.errs, fmt.Errorf("the path %q does not exist", p))
			continue
		}
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("the path %q cannot be accessed: %v", p, err))
			continue
		}

		visitors, err := ExpandPathsToFileVisitors(b.mapper, p, recursive, resource.FileExtensions, b.schema)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("error reading %q: %v", p, err))
		}

		b.paths = append(b.paths, visitors...)
	}
	if len(b.paths) == 0 && len(b.errs) == 0 {
		b.errs = append(b.errs, fmt.Errorf("error reading %v: recognized file extensions are %v", paths, resource.FileExtensions))
	}
	return b
}

// Do returns a Result object with a Visitor for the resources identified by the Builder.
// The visitor will respect the error behavior specified by ContinueOnError. Note that stream
// inputs are consumed by the first execution - use Infos() or Object() on the Result to capture a list
// for further iteration.
func (b *Builder) Do() *Result {
	r := b.visitorResult()
	return r
}

func (b *Builder) visitorResult() *Result {
	if len(b.errs) > 0 {
		return &Result{err: utilerrors.NewAggregate(b.errs)}
	}

	// visit items specified by paths
	if len(b.paths) != 0 {
		return b.visitByPaths()
	}
	return &Result{err: missingResourceError}
}

func (b *Builder) visitByPaths() *Result {
	result := &Result{}

	var visitors Visitor
	if b.continueOnError {
		visitors = EagerVisitorList(b.paths)
	} else {
		visitors = VisitorList(b.paths)
	}

	result.visitor = visitors
	result.sources = b.paths
	return result
}

// expandIfFilePattern returns all the filenames that match the input pattern
// or the filename if it is a specific filename and not a pattern.
// If the input is a pattern and it yields no result it will result in an error.
func expandIfFilePattern(pattern string) ([]string, error) {
	if _, err := os.Stat(pattern); os.IsNotExist(err) {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) == 0 {
			return nil, fmt.Errorf("the path %q does not exist", pattern)
		}
		if err == filepath.ErrBadPattern {
			return nil, fmt.Errorf("pattern %q is not valid: %v", pattern, err)
		}
		return matches, err
	}
	return []string{pattern}, nil
}

var missingResourceError = fmt.Errorf(`you must provide one or more resources`)

// Result contains helper methods for dealing with the outcome of a Builder.
type Result struct {
	err     error
	visitor Visitor

	sources []Visitor
	// populated by a call to Infos
	info []*Info

	ignoreErrors []utilerrors.Matcher
}

// Err returns one or more errors (via a util.ErrorList) that occurred prior
// to visiting the elements in the visitor. To see all errors including those
// that occur during visitation, invoke Infos().
func (r *Result) Err() error {
	return r.err
}

func (r *Result) Objects() ([]interface{}, error) {
	infos, err := r.Infos()
	if err != nil {
		return nil, err
	}

	objects := make([]interface{}, len(infos))
	for i, info := range infos {
		objects[i] = info.Object
	}
	return objects, err
}

// Infos returns an array of all of the resource infos retrieved via traversal.
// Will attempt to traverse the entire set of visitors only once, and will return
// a cached list on subsequent calls.
func (r *Result) Infos() ([]*Info, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.info != nil {
		return r.info, nil
	}

	var infos []*Info
	err := r.visitor.Visit(func(info *Info, err error) error {
		if err != nil {
			return err
		}
		infos = append(infos, info)
		return nil
	})
	err = utilerrors.FilterOut(err, r.ignoreErrors...)

	r.info, r.err = infos, err
	return infos, err
}

// Visit implements the Visitor interface on the items described in the Builder.
// Note that some visitor sources are not traversable more than once, or may
// return different results.  If you wish to operate on the same set of resources
// multiple times, use the Infos() method.
func (r *Result) Visit(fn VisitorFunc) error {
	if r.err != nil {
		return r.err
	}
	err := r.visitor.Visit(fn)
	return utilerrors.FilterOut(err, r.ignoreErrors...)
}
