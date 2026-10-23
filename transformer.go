// Ported from nunjucks/src/transformer.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import "strconv"

// walk visits the tree rooted at n. In pre-order mode (depthFirst false) a replacement returned for a node is
// used without visiting its children; in depth-first mode children are rewritten before the node itself.
func walk(n *node, fn func(*node) *node, depthFirst bool) *node {
	if n == nil {
		return nil
	}
	if !depthFirst {
		if t := fn(n); t != nil && t != n {
			return t
		}
	}
	if n.isList() {
		for i, c := range n.children {
			n.children[i] = walk(c, fn, depthFirst)
		}
	} else {
		for _, f := range n.fieldPtrs() {
			*f = walk(*f, fn, depthFirst)
		}
	}
	if depthFirst {
		if t := fn(n); t != nil {
			return t
		}
	}
	return n
}

// transform applies the nunjucks transformer to a parsed template: super() calls inside blocks are lifted into
// Super nodes, and if/for statements that contain asynchronous constructs become their async forms.
func (env *Environment) transform(root *node) *node {
	return convertStatements(env.liftSuper(root))
}

func (env *Environment) gensym() string {
	s := "hole_" + strconv.Itoa(env.sym)
	env.sym++
	return s
}

func (env *Environment) liftSuper(root *node) *node {
	return walk(root, func(block *node) *node {
		if block.typ != "Block" {
			return nil
		}
		hasSuper := false
		symbol := env.gensym()
		block.body = walk(block.body, func(n *node) *node {
			if (n.typ == "FunCall" || n.typ == "Filter") && n.name != nil && n.name.typ != "LookupVal" && n.name.lit == "super" {
				hasSuper = true
				return newLiteral("Symbol", n.lineno, n.colno, symbol)
			}
			return nil
		}, false)
		if hasSuper {
			super := newNode("Super", 0, 0)
			super.blockName = block.name
			super.symbol = newLiteral("Symbol", 0, 0, symbol)
			block.body.children = append([]*node{super}, block.body.children...)
		}
		return nil
	}, false)
}

func convertStatements(root *node) *node {
	return walk(root, func(n *node) *node {
		if !isIf(n) && !isFor(n) {
			return nil
		}
		async := false
		walk(n, func(child *node) *node {
			switch child.typ {
			case "IfAsync", "AsyncEach", "AsyncAll":
				async = true
				return child
			}
			return nil
		}, false)
		if !async {
			return nil
		}
		if isIf(n) {
			c := *n
			c.typ = "IfAsync"
			return &c
		}
		if n.typ != "AsyncAll" {
			c := *n
			c.typ = "AsyncEach"
			return &c
		}
		return nil
	}, true)
}

func isIf(n *node) bool  { return n.typ == "If" || n.typ == "IfAsync" }
func isFor(n *node) bool { return n.typ == "For" || n.typ == "AsyncEach" || n.typ == "AsyncAll" }
