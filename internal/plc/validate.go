package plc

import (
	"fmt"
	"regexp"
	"strings"
)

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

// Validate performs all structural and type checks before a program is loaded.
// Nothing in an invalid program is ever executed.
func (p *Program) Validate() ([]ValidateError, map[string]Node) {
	var errs []ValidateError
	nodes := make(map[string]Node, len(p.Nodes))
	out := make(map[string][]string)
	in := make(map[string][]string)

	rails := 0
	var railID string
	for _, n := range p.Nodes {
		if n.ID == "" {
			errs = append(errs, ValidateError{Message: "node is missing an id"})
			continue
		}
		if _, dup := nodes[n.ID]; dup {
			errs = append(errs, ValidateError{Message: "duplicate node id", NodeID: n.ID})
		}
		nodes[n.ID] = n
		switch n.Kind {
		case NodeRail:
			rails++
			railID = n.ID
		case NodeContact, NodeNContact, NodeCoil:
			if n.Variable == "" {
				errs = append(errs, ValidateError{Message: string(n.Kind) + " requires a variable", NodeID: n.ID})
			} else if !varNameRe.MatchString(n.Variable) {
				errs = append(errs, ValidateError{Message: "illegal variable name " + n.Variable, NodeID: n.ID})
			} else if n.Kind == NodeCoil && !strings.HasPrefix(n.Variable, "out.") && !strings.HasPrefix(n.Variable, "m.") {
				errs = append(errs, ValidateError{Message: "coil target must be an output (out.*) or internal (m.*)", NodeID: n.ID})
			}
		case NodeTON:
			if n.Variable == "" {
				errs = append(errs, ValidateError{Message: "timer requires a name", NodeID: n.ID})
			} else if !varNameRe.MatchString(n.Variable) {
				errs = append(errs, ValidateError{Message: "illegal timer name " + n.Variable, NodeID: n.ID})
			} else if !strings.HasPrefix(n.Variable, "t.") {
				errs = append(errs, ValidateError{Message: "timer name must use the t.* namespace", NodeID: n.ID})
			}
			if n.DelayMs <= 0 {
				errs = append(errs, ValidateError{Message: "timer delay must be positive", NodeID: n.ID})
			}
		case NodeOR:
		default:
			errs = append(errs, ValidateError{Message: "unknown node kind " + string(n.Kind), NodeID: n.ID})
		}
	}
	if rails == 0 {
		errs = append(errs, ValidateError{Message: "program has no left power rail"})
	} else if rails > 1 {
		errs = append(errs, ValidateError{Message: "program must have exactly one power rail", NodeID: railID})
	}

	edgeIDs := map[string]bool{}
	for _, e := range p.Edges {
		if e.ID == "" {
			errs = append(errs, ValidateError{Message: "wire is missing an id"})
			continue
		}
		if edgeIDs[e.ID] {
			errs = append(errs, ValidateError{Message: "duplicate wire id", EdgeID: e.ID})
		}
		edgeIDs[e.ID] = true
		src, ok1 := nodes[e.Source]
		dst, ok2 := nodes[e.Target]
		if !ok1 {
			errs = append(errs, ValidateError{Message: "wire source does not exist", EdgeID: e.ID})
		}
		if !ok2 {
			errs = append(errs, ValidateError{Message: "wire target does not exist", EdgeID: e.ID})
		}
		if !ok1 || !ok2 {
			continue
		}
		if e.Source == e.Target {
			errs = append(errs, ValidateError{Message: "wire loops back to the same node", EdgeID: e.ID})
		}
		if src.Kind == NodeCoil || src.Kind == NodeTON {
			errs = append(errs, ValidateError{Message: fmt.Sprintf("%s is a terminal element and must not drive another wire", src.Kind), EdgeID: e.ID})
		}
		if dst.Kind == NodeRail {
			errs = append(errs, ValidateError{Message: "power rail cannot be a wire target", EdgeID: e.ID})
		}
		out[e.Source] = append(out[e.Source], e.Target)
		in[e.Target] = append(in[e.Target], e.Source)
	}

	if len(errs) == 0 {
		n := nodes[railID]
		if len(in[n.ID]) != 0 {
			errs = append(errs, ValidateError{Message: "power rail must not have incoming wires", NodeID: n.ID})
		}
		for id, n := range nodes {
			switch n.Kind {
			case NodeRail:
				if len(out[id]) == 0 {
					errs = append(errs, ValidateError{Message: "rail has no outgoing wire", NodeID: id})
				}
			case NodeContact, NodeNContact:
				if len(in[id]) != 1 {
					errs = append(errs, ValidateError{Message: "contact must have exactly one incoming wire; merge parallel branches with an OR node", NodeID: id})
				}
			case NodeOR:
				if len(in[id]) < 2 {
					errs = append(errs, ValidateError{Message: "OR merge needs at least two incoming branches", NodeID: id})
				}
				if len(out[id]) == 0 {
					errs = append(errs, ValidateError{Message: "OR merge has no outgoing wire", NodeID: id})
				}
			case NodeTON:
				if len(in[id]) != 1 {
					errs = append(errs, ValidateError{Message: "timer must have exactly one incoming wire", NodeID: id})
				}
				if len(out[id]) != 0 {
					errs = append(errs, ValidateError{Message: "timer is a terminal element", NodeID: id})
				}
			case NodeCoil:
				if len(in[id]) != 1 {
					errs = append(errs, ValidateError{Message: "coil must have exactly one incoming wire", NodeID: id})
				}
				if len(out[id]) != 0 {
					errs = append(errs, ValidateError{Message: "coil is a terminal element", NodeID: id})
				}
			}
		}
	}

	// Reachability and acyclicity from the rail: power must flow forward only.
	if len(errs) == 0 {
		color := map[string]int{}
		var visit func(id string) bool
		visit = func(id string) bool {
			color[id] = 1
			for _, next := range out[id] {
				if color[next] == 1 {
					errs = append(errs, ValidateError{Message: "wire loop detected: power flow must be acyclic", NodeID: next})
					return false
				}
				if color[next] == 0 && !visit(next) {
					return false
				}
			}
			color[id] = 2
			return true
		}
		visit(railID)
		if len(errs) == 0 {
			for id := range nodes {
				if id != railID && color[id] == 0 {
					errs = append(errs, ValidateError{Message: "element is not reachable from the power rail", NodeID: id})
				}
			}
		}
	}

	// Variables written by coils must not also name a timer.
	if len(errs) == 0 {
		used := map[string]string{}
		for _, n := range nodes {
			switch n.Kind {
			case NodeTON:
				if _, dup := used[n.Variable]; dup {
					errs = append(errs, ValidateError{Message: "duplicate timer name " + n.Variable, NodeID: n.ID})
				}
				used[n.Variable] = string(NodeTON)
			case NodeCoil:
				if _, ok := used[n.Variable]; ok {
					errs = append(errs, ValidateError{Message: "duplicate coil for " + n.Variable + "; each output/internal has one writer", NodeID: n.ID})
				}
				used[n.Variable] = string(NodeCoil)
			}
		}
	}

	return errs, nodes
}
