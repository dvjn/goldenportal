// Package action runs operations on one entity at a time, such as restarting
// a service: a list of steps that call other systems over HTTP or write the
// catalog. Each run is a record anyone allowed can follow, and any portal
// instance can carry it on.
package action

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/auth"
	"github.com/dvjn/goldenportal/internal/blueprint"
	"github.com/dvjn/goldenportal/internal/configdoc"
	"github.com/dvjn/goldenportal/internal/entity"
	"github.com/dvjn/goldenportal/internal/integration"
)

// DocumentKind is how action documents name their kind.
const DocumentKind = "Action"

// Name is the configdoc kind actions are stored as.
const Name = "action"

// Step types.
const (
	StepHTTP     = "http"
	StepEntity   = "entity"
	StepCheck    = "check"
	StepOutput   = "output"
	StepApproval = "approval"
	StepAction   = "action"
)

// WorkflowKind is how workflow documents name their kind.
const WorkflowKind = "Workflow"

// WorkflowName is the configdoc kind workflows are stored as.
const WorkflowName = "workflow"

// maxFanOut caps the entities one action step runs on.
const maxFanOut = 100

// Entity step operations.
const (
	OpUpsert = "upsert"
	OpDelete = "delete"
)

const (
	defaultTimeout = 30 * time.Second
	maxTimeout     = 10 * time.Minute
	maxAttempts    = 5
	maxBackoff     = 5 * time.Minute
	maxApproval    = 30 * 24 * time.Hour
)

// Spec is a parsed action.
type Spec struct {
	// Kind is Action, which runs on one entity of a blueprint, or
	// Workflow, which isn't tied to one and runs actions on many.
	Kind        string `json:"kind"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	// Blueprint is whose entities the action runs on, one at a time.
	Blueprint string `json:"blueprint,omitempty"`
	// Group is where it shows in the Self-Service catalog. Actions without
	// one show under Other.
	Group string `json:"group,omitempty"`
	// Scope narrows the entities it applies to.
	Scope *entity.Filter `json:"scope,omitempty"`
	// Run says who can run it. Admins always can.
	Run     Access               `json:"run"`
	Inputs  []blueprint.Property `json:"inputs"`
	Steps   []Step               `json:"steps"`
	Outputs []Output             `json:"outputs"`
	line    int
}

// Access is who can do something: anyone with one of the roles, in one of
// the groups, or, on an entity matching the filter, such as one their team owns.
type Access struct {
	Roles  []auth.Role    `json:"roles,omitempty"`
	Groups []string       `json:"groups,omitempty"`
	Filter *entity.Filter `json:"filter,omitempty"`
}

// Step is one thing a run does, in order.
type Step struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
	Type  string `json:"type"`
	// Timeout bounds one attempt.
	Timeout Duration `json:"timeout"`
	Retry   *Retry   `json:"retry,omitempty"`
	// Continue keeps the run going when the step fails.
	Continue bool          `json:"continue,omitempty"`
	HTTP     *HTTPStep     `json:"http,omitempty"`
	Entity   *EntityStep   `json:"entity,omitempty"`
	Check    *CheckStep    `json:"check,omitempty"`
	Output   *OutputStep   `json:"output,omitempty"`
	Approval *ApprovalStep `json:"approval,omitempty"`
	Action   *ActionStep   `json:"action,omitempty"`
	line     int
}

// ActionStep runs an action on each of the entities given, as whoever asked
// for the run, who needs the action's run access on each. It waits for them
// all and fails when more than MaxFailures fail. It gives runs, succeeded
// and failed.
type ActionStep struct {
	Name string `json:"name"`
	// Entities is an identifier, or a list of them, of the action's blueprint.
	Entities    any            `json:"entities"`
	Inputs      map[string]any `json:"inputs,omitempty"`
	MaxFailures int            `json:"maxFailures"`
}

// Retry tries a step again after a network error, a 5xx or a 429.
type Retry struct {
	Attempts int      `json:"attempts"`
	Backoff  Duration `json:"backoff"`
}

// HTTPStep calls a URL. Without Output it gives its status and body.
type HTTPStep struct {
	Method  string                             `json:"method"`
	URL     string                             `json:"url"`
	Headers map[string]integration.HeaderValue `json:"-"`
	Body    any                                `json:"body,omitempty"`
	// Success are the statuses that count as working; any 2xx when empty.
	Success []int `json:"success,omitempty"`
	// Output is jq over {status, headers, body}, by name.
	Output []Output `json:"output,omitempty"`
}

// EntityStep writes or deletes an entity as an integration that doesn't pull.
type EntityStep struct {
	Operation   string         `json:"operation"`
	Integration string         `json:"integration"`
	Blueprint   string         `json:"blueprint"`
	Identifier  string         `json:"identifier"`
	Title       string         `json:"title,omitempty"`
	Properties  map[string]any `json:"properties,omitempty"`
}

// CheckStep counts the entities matching a query and fails with Message
// unless the count is as expected.
type CheckStep struct {
	Blueprint string         `json:"blueprint"`
	Filter    *entity.Filter `json:"filter,omitempty"`
	Count     *int           `json:"count,omitempty"`
	Min       *int           `json:"min,omitempty"`
	Max       *int           `json:"max,omitempty"`
	Message   string         `json:"message"`
}

// OutputStep sets what the run shows when it's done.
type OutputStep struct {
	Summary string `json:"summary,omitempty"`
	Links   []Link `json:"links,omitempty"`
}

// ApprovalStep waits until enough approvers approve, showing them what to
// review, such as a plan an earlier step made. One rejection ends the run.
// Whoever asked for the run can't approve it. It gives approvedBy and
// comments.
type ApprovalStep struct {
	Approvers Access   `json:"approvers"`
	Require   int      `json:"require"`
	Timeout   Duration `json:"timeout"`
	Message   string   `json:"message,omitempty"`
	Review    []Review `json:"review,omitempty"`
}

// Review is one thing approvers see: text written in markdown, or a value
// shown as JSON.
type Review struct {
	Title    string `json:"title,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	JSON     any    `json:"json,omitempty"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Output is a named value: jq in an HTTP step, a reference in outputs.
type Output struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// The keys each mapping of an action takes, exported so the documents'
// JSON Schema can be checked against them.
var (
	SpecFields     = []string{"blueprint", "group", "scope", "run", "inputs", "steps", "outputs"}
	AccessFields   = []string{"roles", "groups", "filter"}
	StepFields     = []string{"id", "title", "timeout", "retry", "onFailure", StepHTTP, StepEntity, StepCheck, StepOutput, StepApproval, StepAction}
	ApprovalFields = []string{"approvers", "require", "timeout", "message", "review"}
	ReviewFields   = []string{"title", "markdown", "json"}
	RetryFields    = []string{"attempts", "backoff"}
	HTTPFields     = []string{"method", "url", "headers", "body", "success", "output"}
	EntityFields   = []string{"operation", "integration", "blueprint", "identifier", "title", "properties"}
	CheckFields    = []string{"blueprint", "filter", "expect", "message"}
	ExpectFields   = []string{"count", "min", "max"}
	OutputFields   = []string{"summary", "links"}
	// WorkflowFields are what a workflow document's spec takes.
	WorkflowFields   = []string{"group", "run", "inputs", "steps", "outputs"}
	ActionStepFields = []string{"name", "entities", "inputs", "maxFailures"}
)

var (
	stepTypes   = []string{StepHTTP, StepEntity, StepCheck, StepOutput, StepApproval, StepAction}
	methods     = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,62}$`)
)

func (s Spec) Key() string { return s.Identifier }

func (s Spec) Meta() configdoc.Meta {
	return configdoc.Meta{Title: s.Title, Description: s.Description, Icon: s.Icon, Color: s.Color}
}

func (s Spec) Stats() map[string]int {
	return map[string]int{"inputs": len(s.Inputs), "steps": len(s.Steps)}
}

func (s Spec) Facts() map[string]any {
	facts := map[string]any{"group": s.Group}
	if !s.IsWorkflow() {
		facts["blueprint"] = s.Blueprint
	}

	return facts
}

// IsWorkflow reports whether the spec is a workflow rather than an action.
func (s Spec) IsWorkflow() bool { return s.Kind == WorkflowKind }

// docKind is the configdoc kind the spec is stored as.
func (s Spec) docKind() string {
	if s.IsWorkflow() {
		return WorkflowName
	}

	return Name
}

// Input returns an input by identifier.
func (s Spec) Input(id string) (blueprint.Property, bool) {
	i := slices.IndexFunc(s.Inputs, func(p blueprint.Property) bool { return p.ID == id })
	if i < 0 {
		return blueprint.Property{}, false
	}

	return s.Inputs[i], true
}

// Parse reads an action document and checks everything that doesn't
// depend on other documents.
func Parse(source string) (Spec, []configdoc.Issue) { return parse(source, DocumentKind) }

// ParseWorkflow reads a workflow document. It takes what an action
// does, but no blueprint or scope, and steps can't refer to $entity.
func ParseWorkflow(source string) (Spec, []configdoc.Issue) { return parse(source, WorkflowKind) }

// parseAny reads an action or workflow document, whichever it says it is.
func parseAny(source string) (Spec, []configdoc.Issue) {
	if strings.Contains(source, "kind: "+WorkflowKind) {
		return ParseWorkflow(source)
	}

	return Parse(source)
}

func parse(source, kind string) (Spec, []configdoc.Issue) {
	p := &parser{roots: roots, kind: kind}
	doc := configdoc.ReadDocument(&p.Reader, source, kind)
	spec := Spec{
		Kind: kind, Identifier: doc.Name, Title: doc.Meta.Title, Description: doc.Meta.Description,
		Icon: doc.Meta.Icon, Color: doc.Meta.Color,
		Inputs: []blueprint.Property{}, Steps: []Step{}, Outputs: []Output{},
	}
	if doc.Spec == nil {
		return spec, p.Issues
	}
	allowed := SpecFields
	if spec.IsWorkflow() {
		allowed = WorkflowFields
		p.roots = slices.DeleteFunc(slices.Clone(roots), func(root string) bool { return root == RootEntity })
	}
	fields, ok := p.Fields(doc.Spec, "spec", allowed...)
	if !ok {
		return spec, p.Issues
	}

	spec.line = doc.Spec.Line
	spec.Group = strings.TrimSpace(p.Text(fields["group"], "spec.group"))
	if fields["group"] != nil && spec.Group == "" {
		p.Errorf(fields["group"], "spec.group", "group can't be empty; leave it out to show under %s", OtherGroup)
	}
	if spec.IsWorkflow() {
		if spec.Group == "" && fields["group"] == nil {
			p.Errorf(configdoc.OrNode(fields["group"], doc.Spec), "spec.group", "add the group it shows under in the Self-Service catalog, such as group: Operate")
		}
	} else {
		spec.Blueprint = p.Text(fields["blueprint"], "spec.blueprint")
		spec.line = configdoc.OrNode(fields["blueprint"], doc.Spec).Line
		if !configdoc.ValidIdentifier(spec.Blueprint) {
			p.Errorf(configdoc.OrNode(fields["blueprint"], doc.Spec), "spec.blueprint", "blueprint must name the blueprint whose entities the action runs on")
		}
		if n := fields["scope"]; n != nil {
			spec.Scope = p.filter(n, "spec.scope", RootMe)
		}
	}
	if n := fields["run"]; n == nil {
		p.Errorf(doc.Spec, "spec.run", "say who can run it, such as run: {roles: [member]}; admins always can")
	} else {
		spec.Run = p.access(n, "spec.run")
		if spec.IsWorkflow() && spec.Run.Filter != nil {
			p.Errorf(n, "spec.run.filter", "a workflow runs on no entity, so who can run it is roles and groups; an action it runs checks its own access on each entity")
		}
	}
	spec.Inputs = blueprint.ParseInputs(&p.Reader, fields["inputs"], "spec.inputs")
	p.inputs = spec.Inputs

	p.List(fields["steps"], "spec.steps", func(i int, node *yaml.Node) {
		step := p.step(fmt.Sprintf("spec.steps[%d]", i), node)
		if step.ID != "" {
			p.steps = append(p.steps, step.ID)
		}
		spec.Steps = append(spec.Steps, step)
	})
	if len(spec.Steps) == 0 {
		p.Errorf(configdoc.OrNode(fields["steps"], doc.Spec), "spec.steps", "add the steps it takes")
	}

	p.Each(fields["outputs"], "spec.outputs", func(name string, key, value *yaml.Node) {
		path := "spec.outputs." + name
		if !namePattern.MatchString(name) {
			p.Errorf(key, path, "%q must start with a letter and use only letters, digits and _", name)
		}
		spec.Outputs = append(spec.Outputs, Output{Name: name, Value: p.value(value, path, p.roots...)})
	})

	return spec, p.Issues
}

type parser struct {
	configdoc.Reader
	// kind is the document's kind, and roots what its references can read.
	kind   string
	roots  []string
	inputs []blueprint.Property
	// steps are the identifiers of the steps so far, which later ones can refer to.
	steps []string
}

// value decodes a node and checks the references it makes, which may only
// use the roots given.
func (p *parser) value(node *yaml.Node, path string, allowed ...string) any {
	var value any
	if err := node.Decode(&value); err != nil {
		p.Errorf(node, path, "%v", err)
		return nil
	}
	value = jsonish(value)
	refs, errs := refsIn(value)
	for _, err := range errs {
		p.Errorf(node, path, "%v", err)
	}
	p.checkRefs(node, path, refs, allowed)

	return value
}

func (p *parser) checkRefs(node *yaml.Node, path string, refs []Ref, allowed []string) {
	for _, ref := range refs {
		switch {
		case !slices.Contains(allowed, ref.Root):
			p.Errorf(node, path, "%s can't be used here; use %s", ref, refList(allowed))
		case ref.Root == RootInput:
			if !slices.ContainsFunc(p.inputs, func(in blueprint.Property) bool { return in.ID == ref.Path[0] }) {
				p.Errorf(node, path, "%s names no input; declare it under spec.inputs", ref)
			}
		case ref.Root == RootStep:
			if !slices.Contains(p.steps, ref.Path[0]) {
				p.Errorf(node, path, "%s names no earlier step; give the step id: %s", ref, ref.Path[0])
			}
		}
	}
}

func refList(allowed []string) string {
	out := make([]string, len(allowed))
	for i, root := range allowed {
		out[i] = "$" + root
	}

	return strings.Join(out, ", ")
}

// text reads a string that may hold references.
func (p *parser) text(node *yaml.Node, path string, allowed ...string) string {
	if node == nil {
		return ""
	}
	if node.Kind != yaml.ScalarNode {
		p.Errorf(node, path, "%s must be text", path)
		return ""
	}
	value, _ := p.value(node, path, allowed...).(string)
	if value == "" {
		value = node.Value
	}

	return value
}

func (p *parser) filter(node *yaml.Node, path string, allowed ...string) *entity.Filter {
	var raw any
	if err := node.Decode(&raw); err != nil {
		p.Errorf(node, path, "the filter is not valid: %v", err)
		return nil
	}
	f, err := toFilter(jsonish(raw))
	if err != nil {
		p.Errorf(node, path, "%v", err)
		return nil
	}
	refs, errs := filterRefs(f)
	for _, err := range errs {
		p.Errorf(node, path, "%v", err)
	}
	p.checkRefs(node, path, refs, allowed)

	return f
}

func (p *parser) access(node *yaml.Node, path string) Access {
	a := Access{}
	fields, ok := p.Fields(node, path, AccessFields...)
	if !ok {
		return a
	}
	for _, value := range p.texts(fields["roles"], path+".roles") {
		role, ok := auth.ParseRole(value)
		if !ok {
			p.Errorf(fields["roles"], path+".roles", "role must be member, builder or admin")
		} else if !slices.Contains(a.Roles, role) {
			a.Roles = append(a.Roles, role)
		}
	}
	for _, group := range p.texts(fields["groups"], path+".groups") {
		if strings.TrimSpace(group) == "" {
			p.Errorf(fields["groups"], path+".groups", "group names can't be empty")
		} else if !slices.Contains(a.Groups, group) {
			a.Groups = append(a.Groups, group)
		}
	}
	if n := fields["filter"]; n != nil {
		a.Filter = p.filter(n, path+".filter", RootMe)
	}
	if len(a.Roles)+len(a.Groups) == 0 && a.Filter == nil {
		p.Errorf(node, path, "name the roles or groups that can, or a filter on the entities they can run it on")
	}

	return a
}

func (p *parser) texts(node *yaml.Node, path string) []string {
	if node == nil {
		return nil
	}
	var values []string
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	p.List(node, path, func(_ int, item *yaml.Node) { values = append(values, p.Text(item, path)) })

	return values
}

func (p *parser) duration(node *yaml.Node, path string, limit time.Duration) Duration {
	if node == nil {
		return 0
	}
	d, err := time.ParseDuration(p.Text(node, path))
	if err != nil || d <= 0 {
		p.Errorf(node, path, "%s must be a duration, such as 30s or 2m", path)
		return 0
	}
	if d > limit {
		p.Errorf(node, path, "%s can be at most %s", path, limit)
	}

	return Duration(d)
}

// Duration is a duration that reads in JSON as it's written, such as 30s.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	text := time.Duration(d).String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}

	return json.Marshal(text)
}

func (p *parser) step(path string, node *yaml.Node) Step {
	step := Step{line: node.Line, Timeout: Duration(defaultTimeout)}
	fields, ok := p.Fields(node, path, StepFields...)
	if !ok {
		return step
	}
	if n := fields["id"]; n != nil {
		step.ID = p.Text(n, path+".id")
		switch {
		case !namePattern.MatchString(step.ID):
			p.Errorf(n, path+".id", "id must start with a letter and use only letters, digits and _")
		case slices.Contains(p.steps, step.ID):
			p.Errorf(n, path+".id", "another step already has id %s", step.ID)
		}
	}
	var types []string
	for _, t := range stepTypes {
		if fields[t] != nil {
			types = append(types, t)
		}
	}
	if len(types) != 1 {
		p.Errorf(node, path, "a step does one thing: give it one of %s", strings.Join(stepTypes, ", "))
		return step
	}
	step.Type = types[0]
	step.Title = p.Text(fields["title"], path+".title")
	if n := fields["timeout"]; n != nil {
		step.Timeout = p.duration(n, path+".timeout", maxTimeout)
	}
	if n := fields["retry"]; n != nil {
		step.Retry = p.retry(n, path+".retry")
	}
	if n := fields["onFailure"]; n != nil {
		switch p.Text(n, path+".onFailure") {
		case "continue":
			step.Continue = true
		case "stop":
		default:
			p.Errorf(n, path+".onFailure", "onFailure must be stop or continue")
		}
	}

	typePath := path + "." + step.Type
	body := fields[step.Type]
	switch step.Type {
	case StepHTTP:
		step.HTTP = p.http(body, typePath)
		if step.Title == "" && step.HTTP != nil {
			step.Title = step.HTTP.Method + " " + hostOf(step.HTTP.URL)
		}
	case StepEntity:
		step.Entity = p.entityStep(body, typePath)
		if step.Title == "" && step.Entity != nil {
			step.Title = titleCase(step.Entity.Operation) + " " + step.Entity.Blueprint
		}
	case StepCheck:
		step.Check = p.check(body, typePath)
		if step.Title == "" && step.Check != nil {
			step.Title = "Check " + step.Check.Blueprint
		}
	case StepOutput:
		step.Output = p.output(body, typePath)
		if step.Title == "" {
			step.Title = "Output"
		}
	case StepAction:
		if p.kind != WorkflowKind {
			p.Errorf(body, typePath, "only workflows run actions; an action runs on its own entity")
			return step
		}
		step.Action = p.actionStep(body, typePath)
		if step.Title == "" && step.Action != nil {
			step.Title = "Run " + step.Action.Name
		}
		for _, name := range []string{"timeout", "retry"} {
			if n := fields[name]; n != nil {
				p.Errorf(n, path+"."+name, "action steps take no %s; each action's own steps have theirs", name)
			}
		}
	case StepApproval:
		step.Approval = p.approval(body, typePath)
		if step.Title == "" {
			step.Title = "Approval"
		}
		for _, name := range []string{"timeout", "onFailure"} {
			if n := fields[name]; n != nil {
				p.Errorf(n, path+"."+name, "approval steps take no %s; a rejection ends the run, and approval.timeout says how long to wait", name)
			}
		}
	}
	if step.Type != StepHTTP && step.Retry != nil {
		p.Errorf(fields["retry"], path+".retry", "only http steps retry; the portal's own steps either work or don't")
	}

	return step
}

func (p *parser) retry(node *yaml.Node, path string) *Retry {
	r := &Retry{}
	fields, ok := p.Fields(node, path, RetryFields...)
	if !ok {
		return r
	}
	if n := fields["attempts"]; n == nil || n.Decode(&r.Attempts) != nil || r.Attempts < 1 || r.Attempts > maxAttempts {
		p.Errorf(configdoc.OrNode(n, node), path+".attempts", "attempts must be how many more tries to make, from 1 to %d", maxAttempts)
	}
	if n := fields["backoff"]; n == nil {
		p.Errorf(node, path+".backoff", "add how long to wait before trying again, such as backoff: 10s")
	} else {
		r.Backoff = p.duration(n, path+".backoff", maxBackoff)
	}

	return r
}

func (p *parser) http(node *yaml.Node, path string) *HTTPStep {
	h := &HTTPStep{Headers: map[string]integration.HeaderValue{}}
	fields, ok := p.Fields(node, path, HTTPFields...)
	if !ok {
		return h
	}
	h.Method = strings.ToUpper(p.Text(fields["method"], path+".method"))
	if !slices.Contains(methods, h.Method) {
		p.Errorf(configdoc.OrNode(fields["method"], node), path+".method", "method must be one of %s", strings.Join(methods, ", "))
	}
	h.URL = p.text(fields["url"], path+".url", p.roots...)
	if !strings.HasPrefix(h.URL, "http://") && !strings.HasPrefix(h.URL, "https://") {
		p.Errorf(configdoc.OrNode(fields["url"], node), path+".url", "url must start with http:// or https://")
	}
	h.Headers = integration.ReadHeaders(&p.Reader, fields["headers"], path+".headers")
	for name, header := range h.Headers {
		refs, errs := refsIn(header.Value)
		for _, err := range errs {
			p.Errorf(fields["headers"], path+".headers."+name, "%v", err)
		}
		p.checkRefs(fields["headers"], path+".headers."+name, refs, p.roots)
	}
	if n := fields["body"]; n != nil {
		if h.Method == http.MethodGet {
			p.Errorf(n, path+".body", "GET requests have no body")
		}
		h.Body = p.value(n, path+".body", p.roots...)
	}
	if n := fields["success"]; n != nil {
		if err := n.Decode(&h.Success); err != nil || len(h.Success) == 0 ||
			slices.ContainsFunc(h.Success, func(code int) bool { return code < 100 || code > 599 }) {
			p.Errorf(n, path+".success", "success must list HTTP statuses, such as [200, 202]")
		}
	}
	p.Each(fields["output"], path+".output", func(name string, key, value *yaml.Node) {
		outputPath := path + ".output." + name
		if !namePattern.MatchString(name) {
			p.Errorf(key, outputPath, "%q must start with a letter and use only letters, digits and _", name)
		}
		expression := p.Text(value, outputPath)
		if _, err := compileJQ(expression); err != nil {
			p.Errorf(value, outputPath, "this is not valid jq: %v", err)
		}
		h.Output = append(h.Output, Output{Name: name, Value: expression})
	})

	return h
}

func (p *parser) entityStep(node *yaml.Node, path string) *EntityStep {
	e := &EntityStep{}
	fields, ok := p.Fields(node, path, EntityFields...)
	if !ok {
		return e
	}
	e.Operation = p.Text(fields["operation"], path+".operation")
	if e.Operation != OpUpsert && e.Operation != OpDelete {
		p.Errorf(configdoc.OrNode(fields["operation"], node), path+".operation",
			"operation must be upsert, which creates the entity or updates the properties given, or delete")
	}
	e.Integration = p.Text(fields["integration"], path+".integration")
	if !configdoc.ValidIdentifier(e.Integration) {
		p.Errorf(configdoc.OrNode(fields["integration"], node), path+".integration", "integration must name the integration the entity is written as")
	}
	e.Blueprint = p.Text(fields["blueprint"], path+".blueprint")
	if !configdoc.ValidIdentifier(e.Blueprint) {
		p.Errorf(configdoc.OrNode(fields["blueprint"], node), path+".blueprint", "blueprint must name the entity's blueprint")
	}
	e.Identifier = p.text(fields["identifier"], path+".identifier", p.roots...)
	if strings.TrimSpace(e.Identifier) == "" {
		p.Errorf(configdoc.OrNode(fields["identifier"], node), path+".identifier", "add the entity's identifier, such as $entity or $input.name")
	}
	if e.Operation == OpDelete {
		for _, name := range []string{"title", "properties"} {
			if n := fields[name]; n != nil {
				p.Errorf(n, path+"."+name, "deleting takes no %s", name)
			}
		}
		return e
	}
	e.Title = p.text(fields["title"], path+".title", p.roots...)
	if n := fields["properties"]; n != nil {
		if properties, ok := p.value(n, path+".properties", p.roots...).(map[string]any); ok {
			e.Properties = properties
		} else {
			p.Errorf(n, path+".properties", "properties must be a mapping of property to value")
		}
	}

	return e
}

func (p *parser) check(node *yaml.Node, path string) *CheckStep {
	c := &CheckStep{}
	fields, ok := p.Fields(node, path, CheckFields...)
	if !ok {
		return c
	}
	c.Blueprint = p.Text(fields["blueprint"], path+".blueprint")
	if !configdoc.ValidIdentifier(c.Blueprint) {
		p.Errorf(configdoc.OrNode(fields["blueprint"], node), path+".blueprint", "blueprint must name the blueprint to count")
	}
	if n := fields["filter"]; n != nil {
		c.Filter = p.filter(n, path+".filter", p.roots...)
	}
	if n := fields["expect"]; n == nil {
		p.Errorf(node, path+".expect", "add what the count must be, such as expect: {count: 0} or {min: 1}")
	} else if expect, ok := p.Fields(n, path+".expect", ExpectFields...); ok {
		for name, target := range map[string]**int{"count": &c.Count, "min": &c.Min, "max": &c.Max} {
			if m := expect[name]; m != nil {
				var value int
				if err := m.Decode(&value); err != nil || value < 0 {
					p.Errorf(m, path+".expect."+name, "%s must be a whole number of 0 or more", name)
					continue
				}
				*target = &value
			}
		}
		if c.Count == nil && c.Min == nil && c.Max == nil {
			p.Errorf(n, path+".expect", "expect takes count, min or max")
		}
		if c.Count != nil && (c.Min != nil || c.Max != nil) {
			p.Errorf(n, path+".expect", "expect an exact count, or min and max, not both")
		}
	}
	c.Message = p.text(fields["message"], path+".message", p.roots...)
	if strings.TrimSpace(c.Message) == "" {
		p.Errorf(configdoc.OrNode(fields["message"], node), path+".message", "add the message to show when the check fails, saying what to do")
	}

	return c
}

func (p *parser) output(node *yaml.Node, path string) *OutputStep {
	o := &OutputStep{}
	fields, ok := p.Fields(node, path, OutputFields...)
	if !ok {
		return o
	}
	o.Summary = p.text(fields["summary"], path+".summary", p.roots...)
	p.Each(fields["links"], path+".links", func(label string, _, value *yaml.Node) {
		o.Links = append(o.Links, Link{Label: label, URL: p.text(value, path+".links."+label, p.roots...)})
	})
	if o.Summary == "" && len(o.Links) == 0 {
		p.Errorf(node, path, "add a summary or links")
	}

	return o
}

func (p *parser) approval(node *yaml.Node, path string) *ApprovalStep {
	a := &ApprovalStep{}
	fields, ok := p.Fields(node, path, ApprovalFields...)
	if !ok {
		return a
	}
	if n := fields["approvers"]; n == nil {
		p.Errorf(node, path+".approvers", "say who can approve, such as approvers: {groups: [platform]}")
	} else {
		a.Approvers = p.access(n, path+".approvers")
		if p.kind == WorkflowKind && a.Approvers.Filter != nil {
			p.Errorf(n, path+".approvers.filter", "a workflow runs on no entity, so approvers are roles and groups")
		}
	}
	if n := fields["require"]; n == nil || n.Decode(&a.Require) != nil || a.Require < 1 {
		p.Errorf(configdoc.OrNode(n, node), path+".require", "require must be how many approvals it needs, 1 or more")
	}
	if n := fields["timeout"]; n == nil {
		p.Errorf(node, path+".timeout", "add how long to wait for approval, such as timeout: 24h")
	} else {
		a.Timeout = p.duration(n, path+".timeout", maxApproval)
	}
	a.Message = p.text(fields["message"], path+".message", p.roots...)
	p.List(fields["review"], path+".review", func(i int, item *yaml.Node) {
		reviewPath := fmt.Sprintf("%s.review[%d]", path, i)
		reviewFields, ok := p.Fields(item, reviewPath, ReviewFields...)
		if !ok {
			return
		}
		r := Review{Title: p.Text(reviewFields["title"], reviewPath+".title")}
		switch {
		case reviewFields["markdown"] != nil && reviewFields["json"] != nil:
			p.Errorf(item, reviewPath, "show markdown or json, not both")
		case reviewFields["markdown"] != nil:
			r.Markdown = p.text(reviewFields["markdown"], reviewPath+".markdown", p.roots...)
		case reviewFields["json"] != nil:
			r.JSON = p.value(reviewFields["json"], reviewPath+".json", p.roots...)
		default:
			p.Errorf(item, reviewPath, "add markdown or json to show")
		}
		a.Review = append(a.Review, r)
	})

	return a
}

func (p *parser) actionStep(node *yaml.Node, path string) *ActionStep {
	a := &ActionStep{}
	fields, ok := p.Fields(node, path, ActionStepFields...)
	if !ok {
		return a
	}
	a.Name = p.Text(fields["name"], path+".name")
	if !configdoc.ValidIdentifier(a.Name) {
		p.Errorf(configdoc.OrNode(fields["name"], node), path+".name", "name must be the identifier of the action to run")
	}
	if n := fields["entities"]; n == nil {
		p.Errorf(node, path+".entities", "add the entities to run it on, such as entities: $input.services")
	} else {
		a.Entities = p.value(n, path+".entities", p.roots...)
	}
	if n := fields["inputs"]; n != nil {
		if inputs, ok := p.value(n, path+".inputs", p.roots...).(map[string]any); ok {
			a.Inputs = inputs
		} else {
			p.Errorf(n, path+".inputs", "inputs must be a mapping of the action's inputs to values")
		}
	}
	if n := fields["maxFailures"]; n != nil {
		if err := n.Decode(&a.MaxFailures); err != nil || a.MaxFailures < 0 {
			p.Errorf(n, path+".maxFailures", "maxFailures must be how many runs may fail, 0 or more")
		}
	}

	return a
}

func compileJQ(expression string) (*gojq.Code, error) {
	query, err := gojq.Parse(expression)
	if err != nil {
		return nil, err
	}

	return gojq.Compile(query)
}

func hostOf(url string) string {
	rest := url[strings.Index(url, "//")+2:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}

	return rest
}

func titleCase(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// jsonish turns what YAML decodes into what JSON does, so values compare
// and encode the same either way.
func jsonish(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = jsonish(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = jsonish(item)
		}
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint64:
		return float64(v)
	}

	return value
}
