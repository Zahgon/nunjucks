// Ported from the TemplateError and error helpers of nunjucks/src/lib.js and nunjucks/src/runtime.js of
// mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"fmt"

	"github.com/mozilla/nunjucks/js"
)

// templateError is new lib.TemplateError(message, lineno, colno) with a string message; positions of 0 are
// unset.
func templateError(msg string, lineno, colno int) *js.Error {
	return &js.Error{Name: "Template render error", Message: msg, Lineno: lineno, Colno: colno, Template: true,
		FirstUpdate: true}
}

// wrapError is new lib.TemplateError(cause, lineno, colno) for an Error cause: the message becomes
// "<name>: <message>" of the cause.
func wrapError(cause *js.Error, lineno, colno int) *js.Error {
	return templateError(cause.Name+": "+cause.Message, lineno, colno)
}

// update is TemplateError.prototype.Update: it prefixes the message with the template path, and with the
// position the first time.
func update(e *js.Error, path string) {
	if path == "" {
		path = "unknown path"
	}
	msg := "(" + path + ")"
	if e.FirstUpdate {
		switch {
		case e.Lineno != 0 && e.Colno != 0:
			msg += fmt.Sprintf(" [Line %d, Column %d]", e.Lineno, e.Colno)
		case e.Lineno != 0:
			msg += fmt.Sprintf(" [Line %d]", e.Lineno)
		}
	}
	msg += "\n "
	if e.FirstUpdate {
		msg += " "
	}
	e.Message = msg + e.Message
	e.FirstUpdate = false
}

// prettifyError is lib._prettifyError without the dev flag: errors other than TemplateErrors are wrapped, the
// path is added, and the result is a plain Error carrying the message under the name "Template render error".
func prettifyError(path string, err error) *js.Error {
	e := asJSError(err)
	if !e.Template {
		e = wrapError(e, 0, 0)
	}
	update(e, path)
	return &js.Error{Name: e.Name, Message: e.Message}
}

// handleError is runtime.handleError: an error that already has a line number is kept, anything else is
// wrapped with the position of the running template code.
func handleError(err error, lineno, colno int) *js.Error {
	e := asJSError(err)
	if e.Lineno != 0 {
		return e
	}
	return wrapError(e, lineno, colno)
}

// asJSError returns err as the JavaScript error object it stands for.
func asJSError(err error) *js.Error {
	if e, ok := err.(*js.Error); ok {
		return e
	}
	return js.NewError(err.Error())
}
