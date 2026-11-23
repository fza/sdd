package engine

import (
	"text/template"
	"text/template/parse"
)

// templateReads names the context keys the templates may print: every field a
// pipeline names, as .name or $.name, and every string literal, which index
// may look up. A key an if, with or range only tests is not printed: its
// condition counts only when it declares a variable, or, for with and range,
// when the body uses dot. A field read inside with or range counts too, so the
// set may name more keys than are printed, never fewer.
func templateReads(tmpls []*template.Template) map[string]bool {
	reads := map[string]bool{}
	for _, tmpl := range tmpls {
		for _, assoc := range tmpl.Templates() {
			if assoc.Tree != nil {
				collectReads(assoc.Root, reads)
			}
		}
	}
	return reads
}

func collectReads(node parse.Node, reads map[string]bool) {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			collectReads(child, reads)
		}
	case *parse.ActionNode:
		collectReads(n.Pipe, reads)
	case *parse.IfNode:
		collectBranch(&n.BranchNode, false, reads)
	case *parse.RangeNode:
		collectBranch(&n.BranchNode, true, reads)
	case *parse.WithNode:
		collectBranch(&n.BranchNode, true, reads)
	case *parse.TemplateNode:
		collectReads(n.Pipe, reads)
	case *parse.PipeNode:
		if n == nil {
			return
		}
		for _, cmd := range n.Cmds {
			collectReads(cmd, reads)
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			collectReads(arg, reads)
		}
	case *parse.ChainNode:
		collectReads(n.Node, reads)
	case *parse.FieldNode:
		for _, field := range n.Ident {
			reads[field] = true
		}
	case *parse.VariableNode:
		for _, field := range n.Ident[1:] {
			reads[field] = true
		}
	case *parse.StringNode:
		reads[n.Text] = true
	}
}

func collectBranch(n *parse.BranchNode, rebindsDot bool, reads map[string]bool) {
	if len(n.Pipe.Decl) > 0 || rebindsDot && usesDot(n.List) {
		collectReads(n.Pipe, reads)
	}
	collectReads(n.List, reads)
	collectReads(n.ElseList, reads)
}

func usesDot(node parse.Node) bool {
	switch n := node.(type) {
	case *parse.DotNode, *parse.FieldNode:
		return true
	case *parse.ListNode:
		if n == nil {
			return false
		}
		for _, child := range n.Nodes {
			if usesDot(child) {
				return true
			}
		}
	case *parse.ActionNode:
		return usesDot(n.Pipe)
	case *parse.IfNode:
		return usesDotBranch(&n.BranchNode)
	case *parse.RangeNode:
		return usesDotBranch(&n.BranchNode)
	case *parse.WithNode:
		return usesDotBranch(&n.BranchNode)
	case *parse.TemplateNode:
		return usesDot(n.Pipe)
	case *parse.PipeNode:
		if n == nil {
			return false
		}
		for _, cmd := range n.Cmds {
			if usesDot(cmd) {
				return true
			}
		}
	case *parse.CommandNode:
		for _, arg := range n.Args {
			if usesDot(arg) {
				return true
			}
		}
	case *parse.ChainNode:
		return usesDot(n.Node)
	}
	return false
}

func usesDotBranch(n *parse.BranchNode) bool {
	return usesDot(n.Pipe) || usesDot(n.List) || usesDot(n.ElseList)
}
