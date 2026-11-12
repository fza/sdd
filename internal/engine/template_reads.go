package engine

import (
	"text/template"
	"text/template/parse"
)

// templateReads names the context keys the templates may read: every field a
// pipeline names, as .name or $.name, and every string literal, which index
// may look up. A field read inside with or range counts too, so the set may
// name more keys than are read, never fewer.
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
		collectBranch(&n.BranchNode, reads)
	case *parse.RangeNode:
		collectBranch(&n.BranchNode, reads)
	case *parse.WithNode:
		collectBranch(&n.BranchNode, reads)
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

func collectBranch(n *parse.BranchNode, reads map[string]bool) {
	collectReads(n.Pipe, reads)
	collectReads(n.List, reads)
	collectReads(n.ElseList, reads)
}
