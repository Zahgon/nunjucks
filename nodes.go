// Ported from nunjucks/src/nodes.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import "github.com/mozilla/nunjucks/js"

// node is one node of the template syntax tree. typ is the nunjucks node type name; the node fields of each
// type are listed, in their nunjucks order, by fieldNames.
type node struct {
	typ    string
	lineno int
	colno  int

	children []*node  // NodeList, Root, Group, Array, Dict, KeywordArgs, Output
	lit      js.Value // Literal, Symbol (the name), TemplateData

	target, val, cond, body, else_, arr, name, args, key, value *node
	template, names, symbol, blockName, left, right, expr, def  *node
	start, stop, step                                           *node

	targets       []*node  // Set
	cases         []*node  // Switch
	ops           []*node  // Compare
	withContext   js.Value // Import, FromImport: null, true or false
	ignoreMissing bool     // Include
	opType        string   // CompareOperand
	setBody       *node    // Set: the Capture of a block set (not a node field in nunjucks)
}

var listTypes = map[string]bool{
	"NodeList": true, "Root": true, "Group": true, "Array": true, "Dict": true, "KeywordArgs": true, "Output": true,
}

func (n *node) isList() bool { return listTypes[n.typ] }

// fieldPtrs returns the node-valued fields of n in nunjucks field order; fields holding arrays or plain values
// are not part of the tree walk, as in nunjucks.
func (n *node) fieldPtrs() []**node {
	switch n.typ {
	case "Pair":
		return []**node{&n.key, &n.value}
	case "LookupVal":
		return []**node{&n.target, &n.val}
	case "If", "IfAsync", "InlineIf":
		return []**node{&n.cond, &n.body, &n.else_}
	case "For", "AsyncEach", "AsyncAll":
		return []**node{&n.arr, &n.name, &n.body, &n.else_}
	case "Macro", "Caller":
		return []**node{&n.name, &n.args, &n.body}
	case "Import":
		return []**node{&n.template, &n.target}
	case "FromImport":
		return []**node{&n.template, &n.names}
	case "FunCall", "Filter":
		return []**node{&n.name, &n.args}
	case "Block":
		return []**node{&n.name, &n.body}
	case "Super":
		return []**node{&n.blockName, &n.symbol}
	case "Extends", "Include":
		return []**node{&n.template}
	case "Set":
		return []**node{&n.value}
	case "Switch":
		return []**node{&n.expr, &n.def}
	case "Case":
		return []**node{&n.cond, &n.body}
	case "Capture":
		return []**node{&n.body}
	case "In", "Is", "Or", "And", "Add", "Concat", "Sub", "Mul", "Div", "FloorDiv", "Mod", "Pow":
		return []**node{&n.left, &n.right}
	case "Not", "Neg", "Pos":
		return []**node{&n.target}
	case "Compare", "CompareOperand":
		return []**node{&n.expr}
	case "Slice":
		return []**node{&n.start, &n.stop, &n.step}
	}
	return nil
}

func newNode(typ string, lineno, colno int) *node {
	return &node{typ: typ, lineno: lineno, colno: colno, withContext: js.Null}
}

func newList(typ string, lineno, colno int, children []*node) *node {
	n := newNode(typ, lineno, colno)
	if children == nil {
		children = []*node{}
	}
	n.children = children
	return n
}

func newLiteral(typ string, lineno, colno int, v js.Value) *node {
	n := newNode(typ, lineno, colno)
	n.lit = v
	return n
}

func newBinOp(typ string, lineno, colno int, left, right *node) *node {
	n := newNode(typ, lineno, colno)
	n.left, n.right = left, right
	return n
}

func newUnary(typ string, lineno, colno int, target *node) *node {
	n := newNode(typ, lineno, colno)
	n.target = target
	return n
}

// symbolName is the name of a Symbol node (the value field of a nunjucks Value node).
func (n *node) symbolName() string {
	if n == nil {
		return "undefined"
	}
	if s, ok := n.lit.(string); ok {
		return s
	}
	return js.ToString(n.lit)
}

// findAll collects the nodes of type typ below n (n included), in nunjucks traversal order.
func (n *node) findAll(typ string, results []*node) []*node {
	if n.isList() {
		for _, c := range n.children {
			results = traverse(c, typ, results)
		}
		return results
	}
	for _, f := range n.fieldPtrs() {
		results = traverse(*f, typ, results)
	}
	return results
}

func traverse(c *node, typ string, results []*node) []*node {
	if c == nil {
		return results
	}
	if c.typ == typ {
		results = append(results, c)
	}
	return c.findAll(typ, results)
}
