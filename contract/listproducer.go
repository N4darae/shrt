package contract

import (
	"slices"
	"sort"
	"strings"

	"github.com/N4darae/shrt/catalog"
	"github.com/N4darae/shrt/chain"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func listedMessage(m *catalog.Method) string {
	if m == nil || m.Streaming() || !chain.IsReadOnlyCall(m.FullName) {
		return ""
	}
	fields := m.Output().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.IsList() && f.Kind() == protoreflect.MessageKind {
			return string(f.Message().FullName())
		}
	}
	return ""
}

func returnsMessage(m *catalog.Method, msg string) bool {
	if m == nil || msg == "" {
		return false
	}
	fields := m.Output().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if !f.IsList() && !f.IsMap() && f.Kind() == protoreflect.MessageKind && string(f.Message().FullName()) == msg {
			return true
		}
	}
	return false
}

func creatorOf(msg string, lib *Library, cat *catalog.Catalog) string {
	cands := []string{}
	for _, m := range cat.Methods() {
		if m.Streaming() || chain.IsReadOnlyCall(m.FullName) || !returnsMessage(m, msg) {
			continue
		}
		if _, ok := lib.Get(m.FullName); !ok {
			continue
		}
		cands = append(cands, m.FullName)
	}
	sort.Strings(cands)
	if len(cands) == 1 {
		return cands[0]
	}
	creates := slices.DeleteFunc(cands, func(c string) bool { return !strings.HasPrefix(shortRPC(c), "Create") })
	if len(creates) == 1 {
		return creates[0]
	}
	return ""
}

func nodesReturn(nodes []string, msg string, cat *catalog.Catalog) bool {
	for _, n := range nodes {
		rpc, _ := SplitNode(n)
		if m, err := cat.Lookup(rpc); err == nil && returnsMessage(m, msg) {
			return true
		}
	}
	return false
}

func shortMessage(msg string) string { return msg[strings.LastIndex(msg, ".")+1:] }

type listProducer struct {
	list    string
	msg     string
	creator string
}

func (p *Plan) noteListProducers(found []listProducer) {
	for _, lp := range found {
		id := p.stepOf[lp.list]
		rpc, _ := SplitNode(lp.list)
		if lp.creator != "" {
			p.note("step %s: %s lists %s items, and its contract declares no needs: and nothing it depends on creates one, "+
				"so the plan added %s (%s, whose response carries one) before it: a list of nothing passes whatever the backend "+
				"lists. Declare needs: [%s] on %s to make it explicit",
				id, shortRPC(rpc), shortMessage(lp.msg), p.stepOf[lp.creator], shortRPC(lp.creator), lp.creator, shortRPC(rpc))
			continue
		}
		p.note("step %s: nothing in this chain creates the %s items %s lists, and no rpc with a contract returns one, so the list "+
			"comes back empty and an assertion on its items cannot fail for the right reason: add the step that creates one, "+
			"or declare needs: on %s naming it", id, shortMessage(lp.msg), shortRPC(rpc), shortRPC(rpc))
	}
}

func lintListNeeds(c *RPCContract, m *catalog.Method, lib *Library, cat *catalog.Catalog) (string, bool) {
	msg := listedMessage(m)
	if msg == "" || len(c.Needs) > 0 || nodesReturn(c.DependenciesFor(""), msg, cat) {
		return "", false
	}
	creator := creatorOf(msg, lib, cat)
	if creator == "" {
		return "", false
	}
	return "lists " + shortMessage(msg) + " items and declares no needs: naming the rpc that creates one, so a chain may list " +
		"nothing and pass; " + shortRPC(creator) + " returns one: declare needs: [" + creator + "] " +
		"(contract plan adds it meanwhile, with a note)", true
}
