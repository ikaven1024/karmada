package genericresource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/cli-runtime/pkg/resource"
)

const (
	constSTDINstr       = "STDIN"
	stopValidateMessage = "if you choose to ignore these errors, turn validation off with --validate=false"
)

// VisitorList implements Visit for the sub visitors it contains. The first error
// returned from a child Visitor will terminate iteration.
type VisitorList []Visitor

// Visit implements Visitor
func (l VisitorList) Visit(fn VisitorFunc) error {
	for i := range l {
		if err := l[i].Visit(fn); err != nil {
			return err
		}
	}
	return nil
}

// EagerVisitorList implements Visit for the sub visitors it contains. All errors
// will be captured and returned at the end of iteration.
type EagerVisitorList []Visitor

// Visit implements Visitor, and gathers errors that occur during processing until
// all sub visitors have been visited.
func (l EagerVisitorList) Visit(fn VisitorFunc) error {
	var errs []error
	for i := range l {
		err := l[i].Visit(func(info *Info, err error) error {
			if err != nil {
				errs = append(errs, err)
				return nil
			}
			if err := fn(info, nil); err != nil {
				errs = append(errs, err)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return utilerrors.NewAggregate(errs)
}

// URLVisitor downloads the contents of a URL, and if successful, returns
// an info object representing the downloaded object.
type URLVisitor struct {
	URL *url.URL
	*StreamVisitor
	HttpAttemptCount int
}

func NewURLVisitor(mapper *mapper, httpAttemptCount int, u *url.URL, schema resource.ContentValidator) *URLVisitor {
	return &URLVisitor{
		URL:              u,
		StreamVisitor:    NewStreamVisitor(nil, mapper, u.String(), schema),
		HttpAttemptCount: httpAttemptCount,
	}
}

func (v *URLVisitor) Visit(fn VisitorFunc) error {
	body, err := readHttpWithRetries(httpgetImpl, time.Second, v.URL.String(), v.HttpAttemptCount)
	if err != nil {
		return err
	}
	defer body.Close()
	v.StreamVisitor.Reader = body
	return v.StreamVisitor.Visit(fn)
}

func ignoreFile(path string, extensions []string) bool {
	if len(extensions) == 0 {
		return false
	}
	ext := filepath.Ext(path)
	for _, s := range extensions {
		if s == ext {
			return false
		}
	}
	return true
}

// FileVisitorForSTDIN return a special FileVisitor just for STDIN
func FileVisitorForSTDIN(mapper *mapper, schema resource.ContentValidator) Visitor {
	return &FileVisitor{
		Path:          constSTDINstr,
		StreamVisitor: NewStreamVisitor(nil, mapper, constSTDINstr, schema),
	}
}

// ExpandPathsToFileVisitors will return a slice of FileVisitors that will handle files from the provided path.
// After FileVisitors open the files, they will pass an io.Reader to a StreamVisitor to do the reading. (stdin
// is also taken care of). Paths argument also accepts a single file, and will return a single visitor
func ExpandPathsToFileVisitors(mapper *mapper, paths string, recursive bool, extensions []string, schema resource.ContentValidator) ([]Visitor, error) {
	var visitors []Visitor
	err := filepath.Walk(paths, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			if path != paths && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		// Don't check extension if the filepath was passed explicitly
		if path != paths && ignoreFile(path, extensions) {
			return nil
		}

		visitor := &FileVisitor{
			Path:          path,
			StreamVisitor: NewStreamVisitor(nil, mapper, path, schema),
		}

		visitors = append(visitors, visitor)
		return nil
	})

	if err != nil {
		return nil, err
	}
	return visitors, nil
}

// FileVisitor is wrapping around a StreamVisitor, to handle open/close files
type FileVisitor struct {
	Path string
	*StreamVisitor
}

// Visit in a FileVisitor is just taking care of opening/closing files
func (v *FileVisitor) Visit(fn VisitorFunc) error {
	var f *os.File
	if v.Path == constSTDINstr {
		f = os.Stdin
	} else {
		var err error
		f, err = os.Open(v.Path)
		if err != nil {
			return err
		}
		defer f.Close()
	}

	// TODO: Consider adding a flag to force to UTF16, apparently some
	// Windows tools don't write the BOM
	utf16bom := unicode.BOMOverride(unicode.UTF8.NewDecoder())
	v.StreamVisitor.Reader = transform.NewReader(f, utf16bom)

	return v.StreamVisitor.Visit(fn)
}

// StreamVisitor reads objects from an io.Reader and walks them. A stream visitor can only be
// visited once.
// TODO: depends on objects being in JSON format before being passed to decode - need to implement
// a stream decoder method on runtime.Codec to properly handle this.
type StreamVisitor struct {
	io.Reader
	*mapper

	Source string
	Schema resource.ContentValidator
}

// NewStreamVisitor is a helper function that is useful when we want to change the fields of the struct but keep calls the same.
func NewStreamVisitor(r io.Reader, mapper *mapper, source string, schema resource.ContentValidator) *StreamVisitor {
	return &StreamVisitor{
		Reader: r,
		mapper: mapper,
		Source: source,
		Schema: schema,
	}
}

// Visit implements Visitor over a stream. StreamVisitor is able to distinct multiple resources in one stream.
func (v *StreamVisitor) Visit(fn VisitorFunc) error {
	d := yaml.NewYAMLOrJSONDecoder(v.Reader, 4096)
	for {
		ext := runtime.RawExtension{}
		if err := d.Decode(&ext); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error parsing %s: %v", v.Source, err)
		}
		// TODO: This needs to be able to handle object in other encodings and schemas.
		ext.Raw = bytes.TrimSpace(ext.Raw)
		if len(ext.Raw) == 0 || bytes.Equal(ext.Raw, []byte("null")) {
			continue
		}
		if err := resource.ValidateSchema(ext.Raw, v.Schema); err != nil {
			return fmt.Errorf("error validating %q: %v", v.Source, err)
		}
		info, err := v.infoForData(ext.Raw, v.Source)
		if err != nil {
			if fnErr := fn(info, err); fnErr != nil {
				return fnErr
			}
			continue
		}
		if err = fn(info, nil); err != nil {
			return err
		}
	}
}

type mapper struct {
	newFunc func() interface{}
}

func (m *mapper) infoForData(data []byte, source string) (*Info, error) {
	info := &Info{
		Source: source,
		Data:   data,
		Object: m.newFunc(),
	}

	err := json.Unmarshal(data, info.Object)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// httpget Defines function to retrieve a url and return the results.  Exists for unit test stubbing.
type httpget func(url string) (int, string, io.ReadCloser, error)

// httpgetImpl Implements a function to retrieve a url and return the results.
func httpgetImpl(url string) (int, string, io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, "", nil, err
	}
	return resp.StatusCode, resp.Status, resp.Body, nil
}

// readHttpWithRetries tries to http.Get the v.URL retries times before giving up.
func readHttpWithRetries(get httpget, duration time.Duration, u string, attempts int) (io.ReadCloser, error) {
	var err error
	if attempts <= 0 {
		return nil, fmt.Errorf("http attempts must be greater than 0, was %d", attempts)
	}
	for i := 0; i < attempts; i++ {
		var (
			statusCode int
			status     string
			body       io.ReadCloser
		)
		if i > 0 {
			time.Sleep(duration)
		}

		// Try to get the URL
		statusCode, status, body, err = get(u)

		// Retry Errors
		if err != nil {
			continue
		}

		if statusCode == http.StatusOK {
			return body, nil
		}
		body.Close()
		// Error - Set the error condition from the StatusCode
		err = fmt.Errorf("unable to read URL %q, server reported %s, status code=%d", u, status, statusCode)

		if statusCode >= 500 && statusCode < 600 {
			// Retry 500's
			continue
		} else {
			// Don't retry other StatusCodes
			break
		}
	}
	return nil, err
}

// Info contains temporary info to execute a REST call, or show the results
// of an already completed REST call.
type Info struct {
	// Optional, Source is the filename or URL to template file (.json or .yaml),
	// or stdin to use to handle the resource
	Source string
	// Optional, this is the most recent value returned by the server if available. It will
	// typically be in unstructured or internal forms, depending on how the Builder was
	// defined. If retrieved from the server, the Builder expects the mapping client to
	// decide the final form. Use the AsVersioned, AsUnstructured, and AsInternal helpers
	// to alter the object versions.
	// If Subresource is specified, this will be the object for the subresource.
	Data []byte

	Object interface{}
}
