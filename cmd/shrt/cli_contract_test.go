package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/N4darae/shrt/catalog/catalogtest"
	"github.com/N4darae/shrt/chain"
	"github.com/N4darae/shrt/contract"
	"gopkg.in/yaml.v3"
)

func cliFlowStyled(n *yaml.Node) {
	if n.Kind == yaml.SequenceNode {
		n.Style = yaml.FlowStyle
	}
	for _, c := range n.Content {
		cliFlowStyled(c)
	}
}

func cliRPCs(doc *yaml.Node) *yaml.Node {
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "rpcs" {
			return root.Content[i+1]
		}
	}
	return nil
}

func TestContractInitRerunKeepsWhatTheUserWroteAndCountsTheTODOsLeft(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	var err error
	out := captureStdout(t, func() { err = contractInit([]string{"-all"}) })
	paths, _ := filepath.Glob(".shrt/contracts/*.yaml")
	if err != nil || len(paths) == 0 || !strings.Contains(out, "TODO") {
		t.Fatalf("contract init -all writes overlays full of TODOs and says so: %v\n%s", err, out)
	}
	styled := map[string]string{}
	var target, dropped, kept string
	for _, p := range paths {
		var doc yaml.Node
		if err := yaml.Unmarshal(mustRead(t, p), &doc); err != nil {
			t.Fatal(err)
		}
		cliFlowStyled(&doc)
		if rpcs := cliRPCs(&doc); target == "" && rpcs != nil && len(rpcs.Content) >= 4 {
			target, dropped, kept = p, rpcs.Content[0].Value, rpcs.Content[2].Value
			rpcs.Content = rpcs.Content[2:]
		}
		curated, _ := yaml.Marshal(&doc)
		writeFile(t, p, string(curated))
		styled[p] = string(curated)
	}
	if target == "" {
		t.Fatal("no overlay with two rpcs to curate")
	}
	out = captureStdout(t, func() { err = contractInit([]string{"-all"}) })
	if err != nil {
		t.Fatal(err)
	}
	for p, curated := range styled {
		got := string(mustRead(t, p))
		if p != target && (got != curated || !strings.Contains(out, "unchanged "+p)) {
			t.Errorf("%s did not change, so it is left alone and said unchanged:\n%s", p, out)
		}
	}
	got := string(mustRead(t, target))
	if !strings.Contains(got, dropped+":") || strings.Contains(got, "\n        - ") || strings.Index(got, kept+":") > strings.Index(got, dropped+":") {
		t.Fatalf("the rerun adds %s after the rpcs already there and keeps the flow-style lists:\n%s", dropped, got)
	}
	todo := regexp.MustCompile(`'?TODO[^'\n]*'?`)
	for _, p := range paths {
		writeFile(t, p, todo.ReplaceAllString(string(mustRead(t, p)), "checked"))
	}
	out = captureStdout(t, func() { err = contractInit([]string{"-all"}) })
	left := 0
	for _, p := range paths {
		left += strings.Count(string(mustRead(t, p)), "TODO")
	}
	if err != nil || (left == 0 && strings.Contains(out, "fill every TODO")) || (left > 0 && !strings.Contains(out, strconv.Itoa(left)+" TODO")) {
		t.Fatalf("with %d TODO(s) left the output counts them and asks for none that are not there: %v\n%s", left, err, out)
	}
}

func TestContractInitWithNoDomainListsThemAndExitsZero(t *testing.T) {
	defer shopStatusWorkspace(t)()
	var err error
	out := captureStdout(t, func() { err = contractInit(nil) })
	if err != nil || !strings.Contains(out, "domains in this catalog:") || !strings.Contains(out, "orders") || strings.Contains(out, "usage:") {
		t.Fatalf("contract init with no domain lists the domains and exits 0: %v\n%s", err, out)
	}
}

func TestContractStatusGapsNamesWhatNoChainCallsAndWhatIsOutOfScope(t *testing.T) {
	chdirToFreshCLIWorkspace(t, "http://127.0.0.1:1")
	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.ShopDescriptor()))
	writeFile(t, ".shrt/chains/cli-thing-flow.yaml", "apiVersion: shrt/v1\nname: cli-thing-flow\nsteps:\n    - id: create_product\n      call: ProductService/CreateProduct\n      body:\n          sku: s\n")
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil || !strings.Contains(out, "no chain     shop.orders.v1.OrderService/CreateOrder (repeated request field(s) no chain sends at all: lines)") ||
		strings.Contains(out, "no chain     shop.catalog.v1.ProductService/CreateProduct") ||
		!strings.Contains(out, "no chain     shop.orders.v1.OrderService/WatchOrder") || !strings.Contains(out, "no chain         no chain calls the rpc") {
		t.Fatalf("an rpc no chain calls, and its repeated fields, are gaps, explained in the legend: %v\n%s", err, out)
	}

	writeFile(t, ".shrt/descriptor.binpb", string(catalogtest.RichDescriptor()))
	out = captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	streamed := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "WatchOrder") && strings.HasPrefix(line, "streaming") ||
			strings.Contains(line, "UploadOrders") && strings.HasPrefix(line, "no chain") {
			t.Fatalf("a server-streaming rpc is callable, a client-streaming one is out of scope:\n%s", out)
		}
		if strings.Contains(line, "server-streaming; only its first message is read, so what it sends later (updates on change) is not checked") {
			streamed++
			if !strings.Contains(line, "WatchOrder: ") {
				t.Fatalf("the line names the server-streaming rpc: %q", line)
			}
		}
	}
	if err != nil || streamed != 1 {
		t.Fatalf("one line for the one callable server-streaming rpc, got %d: %v\n%s", streamed, err, out)
	}
	out = captureStdout(t, func() { _ = contractStatus(nil) })
	if strings.Contains(out, "CONTRACT counts ENTRIES") || !strings.Contains(out, "shrt contract status -v") || !strings.Contains(out, "TOTAL") {
		t.Errorf("the table and one line pointing to -v:\n%s", out)
	}
	if out = captureStdout(t, func() { _ = contractStatus([]string{"-v"}) }); !strings.Contains(out, "CONTRACT counts ENTRIES") {
		t.Errorf("-v explains the columns and the scoring:\n%s", out)
	}
	writeFile(t, ".shrt/quality-baseline", "0\n")
	captureStdout(t, func() { err = contractQuality([]string{"-gate", "-baseline", ".shrt/quality-baseline"}) })
	if err == nil || !strings.Contains(err.Error(), "no overlay covers") || strings.Contains(err.Error(), "got vaguer") {
		t.Fatalf("a score raised by uncovered rpcs says so: %v", err)
	}
	if err := contractQuality([]string{"-gate", "-baseline", "no-such-baseline"}); err == nil {
		t.Fatal("-gate with a missing baseline file fails")
	}
	out = captureStdout(t, func() { err = contractQuality([]string{"-json", "-gate", "-baseline", "no-such-baseline"}) })
	if err == nil || !strings.Contains(out, "total_score") {
		t.Fatalf("-json keeps the gate and still emits the report: %v\n%s", err, out)
	}
}

func TestContractStatusGapsLeavesTheConfiguredLoginOut(t *testing.T) {
	dir := shopWorkspace(t, shopConfig+"auth:\n    call: shop.catalog.v1.ProductService/GetProduct\n    body:\n        username: ${env.API_USER}\n        password: ${env.API_PASSWORD}\n    token_path: access_token\n")
	for domain, rpc := range map[string]string{"auth": "shop.catalog.v1.ProductService/GetProduct", "customers": "shop.customers.v1.CustomerService/CreateCustomer"} {
		writeFile(t, filepath.Join(dir, ".shrt", "contracts", domain+".yaml"), "apiVersion: shrt/contract/v1\ndomain: "+domain+"\nrpcs:\n    "+rpc+":\n        summary: s\n        required: [NONE]\n        status: draft\n")
	}
	defer chdir(t, dir)()
	var err error
	out := captureStdout(t, func() { err = contractStatus([]string{"-gaps"}) })
	if err != nil || strings.Contains(out, "no path to   shop.catalog.v1.ProductService/GetProduct") ||
		!strings.Contains(out, "no path to   shop.customers.v1.CustomerService/CreateCustomer") || !strings.Contains(out, "login") {
		t.Fatalf("the configured login needs no path, other rpcs are still listed, and the legend says why: %v\n%s", err, out)
	}
}

func TestContractQualityNamesTheGapsAndRefusesALibraryThatDoesNotLint(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "quality-baseline")
	writeFile(t, baseline, "0\n")
	err := qualityGate(contract.QualityReport{TotalScore: 2, RPCs: []contract.QualityRPC{
		{Domain: "orders", RPC: "shop.orders.v1.OrderService/CreateOrder", UndocumentedFields: []string{"note"}, Score: 2},
	}}, baseline)
	if err == nil || strings.Contains(err.Error(), "see what got vaguer") {
		t.Fatalf("a field the descriptor gained fails the gate without calling the contract vaguer: %v", err)
	}
	for _, want := range []string{"CreateOrder", "undocumented field(s): note", "descriptor gained"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the gate lacks %q: %v", want, err)
		}
	}
	defer shopStatusWorkspace(t)()
	writeFile(t, filepath.Join(".shrt", "contracts", "orders.yaml"), `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [NONE]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.no_such_field
        status: draft
`)
	captureStdout(t, func() { err = contractLint(nil) })
	if err == nil || !strings.Contains(err.Error(), "contract error(s)") {
		t.Fatalf("a from: naming a field the response lacks is a contract error: %v", err)
	}
	for _, args := range [][]string{nil, {"-json"}} {
		out := captureStdout(t, func() { err = contractQuality(args) })
		if strings.Contains(out, "no rpc in this library has a measurable gap") || err == nil || !strings.Contains(err.Error(), "contract error") {
			t.Fatalf("quality %v refuses to score a library with contract errors: %v\n%s", args, err, out)
		}
	}
}

func TestEveryCommandThatReadsContractsRefusesAnOverlayThatDoesNotParse(t *testing.T) {
	defer shopStatusWorkspace(t)()
	writeFile(t, filepath.Join(".shrt", "contracts", "customers.yaml"), "apiVersion: shrt/contract/v1\ndomain: customers\nrpcs:\n  bad: [\n")
	writeFile(t, filepath.Join(".shrt", "chains", "orders.yaml"), "apiVersion: shrt/v1\nname: orders\nsteps:\n    - id: create\n      call: shop.orders.v1.OrderService/CreateOrder\n      body: {}\n      expect:\n          - path: status.code\n            equals: SUCCESS\n")
	for name, fn := range map[string]func() error{
		"chain lint -strict":           func() error { return chainLint([]string{"-strict"}) },
		"contract plan":                func() error { return contractPlan([]string{"ConfirmOrder"}) },
		"contract lint -domain orders": func() error { return contractLint([]string{"-domain", "orders"}) },
		"contract lint orders -json":   func() error { return contractLint([]string{"-json", "orders"}) },
		"contract show":                func() error { return contractShow([]string{"ConfirmOrder"}) },
		"contract status":              func() error { return contractStatus(nil) },
		"contract quality":             func() error { return contractQuality(nil) },
		"contract init":                func() error { return contractInit([]string{"orders", "-stdout"}) },
		"chain new":                    func() error { return chainNew([]string{"-name", "x", "ConfirmOrder"}) },
		"chain which":                  func() error { return chainWhich([]string{"-rpc", "CreateOrder"}) },
		"chain slice":                  func() error { return chainSlice(t.Context(), []string{"orders", "-step", "create"}) },
	} {
		var err error
		out := captureStdout(t, func() { err = fn() })
		if err == nil || !strings.Contains(fmt.Sprint(err)+out, "customers.yaml") || !strings.Contains(fmt.Sprint(err)+out, "yaml: line") {
			t.Errorf("%s: an overlay that does not parse fails the command, naming the file and its parse error: %v\n%s", name, err, out)
		}
	}
}

const cancelFlowContracts = `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/ConfirmOrder:
        summary: confirms an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/FetchOrder:
        summary: reads an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
    shop.orders.v1.OrderService/CancelOrder:
        summary: cancels an order
        required: [id_order]
        fields:
            id_order:
                from: shop.orders.v1.OrderService/CreateOrder->order.id_order
        aliases:
            confirmed:
                fields:
                    id_order:
                        from: shop.orders.v1.OrderService/ConfirmOrder->order.id_order
        status: draft
`

func TestContractPlanTakesAnAliasAndSeveralTargets(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "orders.yaml"), cancelFlowContracts)
	defer chdir(t, dir)()
	var err error
	planned := func(args ...string) string {
		t.Helper()
		out := captureStdout(t, func() { err = contractPlan(append(args, "-write", "-force")) })
		path := filepath.Join(dir, ".shrt", "chains", strings.Fields(strings.TrimPrefix(out, "wrote .shrt/chains/"))[0])
		raw, rerr := os.ReadFile(strings.TrimSuffix(path, ":"))
		if err != nil || rerr != nil {
			t.Fatalf("plan %v: %v %v\n%s", args, err, rerr, out)
		}
		return out + string(raw)
	}
	if out := planned("CancelOrder@confirmed"); !strings.Contains(out, "order CreateOrder -> ConfirmOrder -> CancelOrder@confirmed") ||
		!strings.Contains(out, "name: orders-cancelorder-confirmed") || !strings.Contains(out, "id_order: ${confirm_order.order.id_order}") {
		t.Fatalf("the aliased target is planned with its overrides:\n%s", out)
	}
	if out := planned("ConfirmOrder", "FetchOrder", "CancelOrder@confirmed"); !strings.Contains(out, "order CreateOrder -> ConfirmOrder -> FetchOrder -> CancelOrder@confirmed\n") ||
		!strings.Contains(out, "name: orders-confirmorder-fetchorder-cancelorder-confirmed") {
		t.Fatalf("several targets compose one deduplicated chain in dependency order, named after every target:\n%s", out)
	}
	out := captureStdout(t, func() { err = contractPlan([]string{"CancelOrder@confirmed"}) })
	if err != nil || !strings.Contains(out, "order: CreateOrder -> ConfirmOrder -> CancelOrder@confirmed\n") ||
		!strings.Contains(out, " steps: 2 setup, 1 target") || strings.Contains(out, "create_order,") || strings.Contains(out, "apiVersion:") {
		t.Fatalf("without -write the plan prints its order and step count per group: %v\n%s", err, out)
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"CancelOrder@confirmed", "-v"}) })
	if err != nil || !strings.Contains(out, "step ids: create_order, confirm_order, ") {
		t.Fatalf("-v prints every step id: %v\n%s", err, out)
	}
}

func TestContractPlanAllPlansOneChainPerRPCWithAContract(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	defer chdir(t, dir)()
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "shop.yaml"), `apiVersion: shrt/contract/v1
domain: shop
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        status: draft
    shop.catalog.v1.ProductService/GetProduct:
        summary: reads a product
        fields:
            id_product: {from: shop.catalog.v1.ProductService/CreateProduct->product.id_product}
        failures:
            - code: 1204
              reason: ProductNotFound
              when: no product has this id
        status: draft
    shop.orders.v1.OrderService/WatchOrder:
        summary: streams an order
        status: draft
`)
	var err error
	out := captureStdout(t, func() { err = contractPlan([]string{"-all"}) })
	for _, want := range []string{"catalog-createproduct: ", "catalog-getproduct: ", "orders-watchorder: "} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("one line per rpc with a contract, want %q: %v\n%s", want, err, out)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".shrt", "chains")); len(entries) != 0 {
		t.Fatalf("without -write nothing is written, found %d file(s)", len(entries))
	}
	if out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) }); err != nil || strings.Count(out, ", written") != 3 {
		t.Fatalf("plan -all -write writes each chain: %v\n%s", err, out)
	}
	path := filepath.Join(dir, ".shrt", "chains", "catalog-getproduct.yaml")
	writeFile(t, path, "edited")
	out = captureStdout(t, func() { err = contractPlan([]string{"-all", "-write"}) })
	if raw, _ := os.ReadFile(path); err != nil || string(raw) != "edited" || !strings.Contains(out, "3 existing chain file(s) kept") {
		t.Fatalf("an existing chain is kept without -force: %v\n%s", err, out)
	}
	captureStdout(t, func() { err = contractPlan([]string{"-all", "-write", "-force"}) })
	if raw, _ := os.ReadFile(path); err != nil || string(raw) == "edited" {
		t.Fatalf("-force overwrites: %v", err)
	}
	if err := contractPlan([]string{"-all", "GetProduct"}); err == nil {
		t.Fatal("-all names no rpc")
	}
	out = captureStdout(t, func() { err = contractPlan([]string{"GetProduct", "-notes"}) })
	if err != nil || !regexp.MustCompile(`(?m)^[a-z ]+ \(1 step\)(, \d+ notes)?: `).MatchString(out) {
		t.Fatalf("-notes prints one line per probe group, counting its steps: %v\n%s", err, out)
	}
}

func TestContractPlanPrintsEachGapInFullBeforeOneLinePerProbeGroup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join("..", "..", "contract", "testdata", "shopdemo")
	writeFile(t, filepath.Join(dir, ".shrt", "descriptor.binpb"), string(mustRead(t, filepath.Join(src, "descriptor.binpb"))))
	writeFile(t, filepath.Join(dir, ".shrt", "config.yaml"), shopConfig+"conventions:\n    envelope_path: status.code\n    envelope_ok: SUCCESS\n")
	for _, name := range []string{"catalog.yaml", "customers.yaml", "orders.yaml"} {
		text := strings.Replace(string(mustRead(t, filepath.Join(src, "contracts", name))), "        needs: [shop.catalog.v1.StockService/AddStock]\n", "", 1)
		writeFile(t, filepath.Join(dir, ".shrt", "contracts", name), text)
	}
	defer chdir(t, dir)()
	gaps := func(out, under string) string {
		lines := []string{}
		in := under == ""
		for _, l := range strings.Split(out, "\n") {
			if under != "" && !strings.HasPrefix(l, " ") {
				in = strings.HasPrefix(l, under+": ")
			}
			if t := strings.TrimSpace(l); in && strings.HasPrefix(t, "gap: ") {
				lines = append(lines, t)
			}
		}
		return strings.Join(lines, "\n")
	}
	var err1, err2, err3, err4 error
	all := captureStdout(t, func() { err1 = contractPlan([]string{"-all"}) })
	one := captureStdout(t, func() { err2 = contractPlan([]string{"ConfirmOrder"}) })
	notes := captureStdout(t, func() { err3 = contractPlan([]string{"ConfirmOrder", "-notes"}) })
	full := captureStdout(t, func() { err4 = contractPlan([]string{"ConfirmOrder", "-notes", "-v"}) })
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		t.Fatalf("plan: %v %v %v %v", err1, err2, err3, err4)
	}
	if want := gaps(one, ""); !strings.Contains(want, "gap: step confirm_order: no write in the chain adds a known quantity") || gaps(all, "orders-confirmorder") != want {
		t.Fatalf("plan ConfirmOrder names the exact-stock gap and -all prints the same gaps:\n%s\n---\n%s", all, one)
	}
	if !strings.Contains(notes, "\ngap: step confirm_order: no write in the chain adds a known quantity") || strings.Contains(notes, "step ids: ") ||
		strings.Contains(notes, "\nnote: ") || len(notes) >= len(full) || !strings.Contains(notes, "-notes -v") {
		t.Fatalf("-notes labels gaps and prints no step ids or full notes, pointing at -notes -v:\n%s", notes)
	}
	if !strings.Contains(full, "step ids: ") || !strings.Contains(full, "\nnote: ") {
		t.Fatalf("-notes -v prints every note in full and every step id:\n%s", full)
	}
	group := regexp.MustCompile(`^[a-z -]+ \(\d+ steps?\)`)
	seenGroup := false
	for _, line := range strings.Split(strings.TrimSpace(notes), "\n")[2:] {
		if strings.HasPrefix(line, "gap: ") {
			if seenGroup {
				t.Fatalf("gap lines come before the probe groups:\n%s", notes)
			}
			continue
		}
		seenGroup = seenGroup || group.MatchString(line)
		if len(line) > 160 {
			t.Fatalf("a probe group line is one short line: %q", line)
		}
	}
	if !seenGroup {
		t.Fatalf("-notes prints one line per probe group:\n%s", notes)
	}
	if !strings.Contains(one, "plan the rpc that adds it (needs:)\n") {
		t.Errorf("a gap says how to close it, so it is never clipped:\n%s", one)
	}
}

func TestChainNewCountsTheStepsItWroteAndPrintsTheLeadOfEachNote(t *testing.T) {
	dir := shopWorkspace(t, shopConfig)
	writeFile(t, filepath.Join(dir, ".shrt", "contracts", "orders.yaml"), `apiVersion: shrt/contract/v1
domain: orders
rpcs:
    shop.catalog.v1.ProductService/CreateProduct:
        summary: adds a product
        required: [NONE]
        status: draft
    shop.orders.v1.OrderService/CreateOrder:
        summary: records an order
        required: [lines]
        fields:
            lines.id_product:
                from: shop.catalog.v1.ProductService/CreateProduct->product.id_product
            lines.qty:
                value: "2"
        effects:
            total_minor: {sum: lines.qty, times: price_minor}
        status: draft
`)
	defer chdir(t, dir)()
	var err error
	out := captureStdout(t, func() { err = chainNew([]string{"-name", "order", "CreateProduct", "CreateOrder"}) })
	c, lerr := chain.LoadFile(filepath.Join(".shrt", "chains", "order.yaml"))
	if err != nil || lerr != nil || len(c.Steps) < 3 || !strings.Contains(out, fmt.Sprintf("(%d step(s))", len(c.Steps))) {
		t.Fatalf("chain new counts the steps it wrote: %v %v\n%s", err, lerr, out)
	}
	for note, want := range map[string]string{
		"steps a, b: x.created_at is asserted within 300s of ${nowunix}: stamped by this call; verify masks it":                  "steps a, b: x.created_at is asserted within 300s of ${nowunix}",
		"step c: asserts only the verdict, though the contract for S/C declares what its response carries (x). The verdict says": "step c: asserts only the verdict, though the contract for S/C declares what its response carries",
		"step c: id wants S/P but that rpc is not in the plan":                                                                   "step c: id wants S/P but that rpc is not in the plan",
	} {
		if got := noteLead(note); got != want {
			t.Errorf("noteLead(%q) = %q, want %q", note, got, want)
		}
	}
}
