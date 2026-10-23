package js

import "fmt"

// Error is a thrown JavaScript error object. Returning an *Error from an operation is the Go form of a
// JavaScript throw. Lineno and Colno carry the nunjucks lib.TemplateError position (0 is unset); Template marks
// a TemplateError, the only kind of error that has the Update method.
type Error struct {
	Name     string
	Message  string
	Lineno   int
	Colno    int
	Template bool
	// FirstUpdate is true until Update has added the template path once.
	FirstUpdate bool
}

// NewError returns a plain Error.
func NewError(msg string) *Error { return &Error{Name: "Error", Message: msg} }

// NewTypeError returns a TypeError with a formatted message.
func NewTypeError(format string, args ...any) *Error {
	return &Error{Name: "TypeError", Message: fmt.Sprintf(format, args...)}
}

// NewRangeError returns a RangeError with a formatted message.
func NewRangeError(format string, args ...any) *Error {
	return &Error{Name: "RangeError", Message: fmt.Sprintf(format, args...)}
}

// NewSyntaxError returns a SyntaxError with a formatted message.
func NewSyntaxError(format string, args ...any) *Error {
	return &Error{Name: "SyntaxError", Message: fmt.Sprintf(format, args...)}
}

// String is Error.prototype.toString.
func (e *Error) String() string {
	switch {
	case e.Name == "":
		return e.Message
	case e.Message == "":
		return e.Name
	}
	return e.Name + ": " + e.Message
}

func (e *Error) Error() string { return e.String() }

// ReadPropertyError is the TypeError V8 throws when a property of null or undefined is read.
func ReadPropertyError(base Value, key string) *Error {
	return NewTypeError("Cannot read properties of %s (reading '%s')", ToString(base), key)
}

// ReadPropertyErrorKey is ReadPropertyError for a key value as V8 shows it: objects as #<Constructor>, arrays
// not at all.
func ReadPropertyErrorKey(base Value, key Value) *Error {
	switch key.(type) {
	case *Array:
		return NewTypeError("Cannot read properties of %s", ToString(base))
	case *Object:
		return ReadPropertyError(base, "#<Object>")
	case *SafeString:
		return ReadPropertyError(base, "#<SafeString>")
	}
	return ReadPropertyError(base, ToString(key))
}

// NotFunctionError is the TypeError V8 throws when the callee expression expr is not callable.
func NotFunctionError(expr string) *Error {
	return NewTypeError("%s is not a function", expr)
}
