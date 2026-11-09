package engine

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/serveview"
	"github.com/networkteam/sdd/internal/truncate"
)

// Delegate child sessions (20260827-232430-d-cpt-voa, refined to rounds by
// 20261109-004845-d-cpt-ldx): a task that delivers runs in its own session,
// forked from the instance that dispatched it. Values cross between the two
// sessions only as deliveries, and a delivery never moves the receiver's step.

const (
	// EventForked records on the dispatching instance that a delegate child
	// session was forked for it, with the field mappings both directions use.
	EventForked EventType = "forked"
	// EventForkEnded records that a fork stopped being live without a final
	// delivery: its child concluded, or the dispatching side ended it.
	EventForkEnded EventType = "fork_ended"
	// EventDelivered records a sent delivery, after the receiver accepted or
	// refused it.
	EventDelivered EventType = "delivered"
	// EventReceived records an accepted delivery.
	EventReceived EventType = "received"
)

// Link names one instance of one session.
type Link struct {
	Session  string
	Instance string
}

// Delivery is one set of values crossing from a sender instance to a
// receiver instance of another session. Field names are the receiver's own:
// the dispatching side maps names in both directions.
type Delivery struct {
	// ID is stable for the sender's nth delivery, so a delivery repeated after
	// a failed sender append is recognized and applied once.
	ID     string
	From   Link
	To     Link
	Fields map[string]any
	// Final marks the delegate's last delivery: the one that completes it.
	Final bool
}

// Courier carries a delivery to the receiving session and returns its
// verdict before the sender records the delivery. A refusal is a receiver
// that can no longer take it (ended move, concluded fork): the sender records
// it as a named notice. An error is a delivery that may not have landed: the
// sender's operation fails and nothing about the sender moves.
type Courier interface {
	Deliver(Delivery) (refusal string, err error)
}

// ForkStatus is a fork's lifecycle as the dispatching instance knows it.
type ForkStatus string

const (
	ForkLive      ForkStatus = "live"
	ForkCompleted ForkStatus = "completed"
	ForkConcluded ForkStatus = "concluded"
)

// Fork is one delegate child session a dispatching instance forked.
type Fork struct {
	Child     Link
	Procedure string
	// Receive maps parent field ← child field; Send maps child field ← parent
	// field (see Dispatch).
	Receive map[string]string
	Send    map[string]string
	Status  ForkStatus
	Reason  string
}

// LiveFork is a fork still live, with the dispatching instance it belongs to.
type LiveFork struct {
	Instance string
	Fork     Fork
}

// ExchangeItem is one delivery an instance sent or accepted.
type ExchangeItem struct {
	Sent    bool
	Fields  map[string]any
	Final   bool
	Refused string
}

// WithLineage opens the session as a delegate child of the dispatching
// instance: non-interactive, driven by handle, running only its task.
func WithLineage(parent Link) SessionOption {
	return func(s *Session) {
		lineage := parent
		s.Lineage = &lineage
	}
}

// WithCourier sets how the session's deliveries reach other sessions.
func WithCourier(courier Courier) SessionOption {
	return func(s *Session) { s.courier = courier }
}

// NonInteractive reports whether no user is present: the session is a
// delegate child session.
func (s *Session) NonInteractive() bool { return s.Lineage != nil }

// NextInstanceID returns the handle the next started instance receives, so a
// fork can be recorded on the dispatching side before its child starts.
func (s *Session) NextInstanceID() string {
	return fmt.Sprintf("i_%d", s.counter+1)
}

// Fork records on the dispatching instance a delegate child session for spec,
// under the mappings its last answered dispatch option declared, and returns
// the seed values the child starts with. Every field the child delivers must
// be received and every field it awaits must be sent, so no delivery is
// undeliverable by construction and no gate waits on a value nothing sends.
func (s *Session) Fork(instanceID string, child Link, spec *Spec) (map[string]any, error) {
	if err := s.checkProgression(); err != nil {
		return nil, err
	}
	p, ok := s.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("no parent instance %q", instanceID)
	}
	if p.Status != StatusRunning {
		return nil, fmt.Errorf("instance %s has ended (%s) — an ended move dispatches no delegate", instanceID, p.Outcome)
	}
	if p.dispatchProcedure != "" && p.dispatchProcedure != spec.Canonical {
		return nil, fmt.Errorf("the option %s last answered dispatches %s, not %s", instanceID, p.dispatchProcedure, spec.Canonical)
	}
	received := map[string]bool{}
	for _, childField := range p.dispatchReceive {
		received[childField] = true
	}
	for _, f := range spec.DeliveredFields() {
		if !received[f] {
			return nil, fmt.Errorf("%s delivers %q, which the option %s last answered does not receive — declare it under the option's dispatch receive", spec.Canonical, f, instanceID)
		}
	}
	awaited := spec.AwaitedFields()
	for _, f := range awaited {
		if _, ok := p.dispatchSend[f]; !ok {
			return nil, fmt.Errorf("%s awaits %q, which the option %s last answered never sends — declare it under the option's dispatch send", spec.Canonical, f, instanceID)
		}
	}
	for childField := range p.dispatchSend {
		if !slices.Contains(awaited, childField) {
			return nil, fmt.Errorf("the option %s last answered sends %q, which %s does not await", instanceID, childField, spec.Canonical)
		}
	}
	seed, err := seedValues(p, spec)
	if err != nil {
		return nil, err
	}
	fork := &Fork{
		Child: child, Procedure: spec.Canonical, Status: ForkLive,
		Receive: maps.Clone(p.dispatchReceive), Send: maps.Clone(p.dispatchSend),
	}
	s.appendEvent(p.ID, EventForked, map[string]any{
		"child": linkData(child), "procedure": spec.Canonical,
		"receive": stringMapData(fork.Receive), "send": stringMapData(fork.Send),
	})
	if err := s.checkSink(); err != nil {
		return nil, err
	}
	p.forks = append(p.forks, fork)
	return seed, nil
}

// EndFork records that a live fork stopped without its final delivery. A
// fork no longer live is left as it is.
func (s *Session) EndFork(instanceID string, child Link, reason string) error {
	if err := s.checkSink(); err != nil {
		return err
	}
	p, ok := s.instances[instanceID]
	if !ok {
		return fmt.Errorf("no instance %q", instanceID)
	}
	fork := p.fork(child)
	if fork == nil {
		return fmt.Errorf("instance %s forked no delegate %s/%s", instanceID, child.Session, child.Instance)
	}
	if fork.Status != ForkLive {
		return nil
	}
	fork.Status, fork.Reason = ForkConcluded, reason
	s.appendEvent(p.ID, EventForkEnded, map[string]any{"child": linkData(child), "reason": reason})
	return s.checkSink()
}

// LiveForks lists every fork still live, in instance start order.
func (s *Session) LiveForks() []LiveFork {
	var out []LiveFork
	for _, id := range s.order {
		for _, fork := range s.instances[id].forks {
			if fork.Status == ForkLive {
				out = append(out, LiveFork{Instance: id, Fork: *fork})
			}
		}
	}
	return out
}

// Exchange returns the deliveries the instance sent and accepted, oldest
// first.
func (s *Session) Exchange(instanceID string) []ExchangeItem {
	inst, ok := s.instances[instanceID]
	if !ok {
		return nil
	}
	return slices.Clone(inst.exchange)
}

// Poll cascades the instance from where it stands without writing anything,
// then serves it: the pickup call after a delivery landed, and a clean
// re-serve while none has.
func (s *Session) Poll(instanceID string) (*Serve, error) {
	if err := s.checkProgression(); err != nil {
		return nil, err
	}
	inst, ok := s.instances[instanceID]
	if !ok {
		return nil, fmt.Errorf("no instance %q", instanceID)
	}
	if inst.Status != StatusRunning {
		return nil, fmt.Errorf("instance %s has ended (%s)", instanceID, inst.Outcome)
	}
	if err := s.cascade(inst); err != nil {
		return nil, err
	}
	return s.serve(inst)
}

// Receive accepts a delivery into the named instance without moving its step.
// From the dispatching instance a delegate session receives only fields its
// gates await; a dispatching instance receives only from a live fork, through
// the fork's receive mapping. A delivery already accepted is accepted again
// as a no-op. An ended receiver refuses; a delivery no declaration covers is
// an error.
func (s *Session) Receive(d Delivery) (string, error) {
	if err := s.checkSink(); err != nil {
		return "", err
	}
	if s.received[d.ID] {
		return "", nil
	}
	inst, ok := s.instances[d.To.Instance]
	if !ok {
		return "", fmt.Errorf("%s holds no instance %q", s.ID, d.To.Instance)
	}
	if inst.Status != StatusRunning {
		return fmt.Sprintf("the receiving move %s has ended (%s)", inst.Spec.Canonical, inst.Outcome), nil
	}
	fields := map[string]any{}
	var fork *Fork
	if s.Lineage != nil && *s.Lineage == d.From {
		awaited := inst.Spec.AwaitedFields()
		for name, v := range d.Fields {
			if !slices.Contains(awaited, name) {
				return "", fmt.Errorf("%s does not await %q — the dispatching move sent a field no gate waits for", inst.Spec.Canonical, name)
			}
			fields[name] = v
		}
	} else {
		fork = inst.fork(d.From)
		if fork == nil {
			return "", fmt.Errorf("instance %s forked no delegate %s/%s", inst.ID, d.From.Session, d.From.Instance)
		}
		if fork.Status != ForkLive {
			return fmt.Sprintf("the delegate fork of %s has ended (%s)", inst.Spec.Canonical, fork.Status), nil
		}
		for childField, v := range d.Fields {
			parentField, ok := fork.receiveField(childField)
			if !ok {
				return "", fmt.Errorf("%s does not receive the delegate's %q", inst.Spec.Canonical, childField)
			}
			fields[parentField] = v
		}
	}
	if _, err := inst.Store.WriteState(fields); err != nil {
		return "", fmt.Errorf("delivery %s into %s: %w", d.ID, inst.ID, err)
	}
	s.applyReceived(inst, d, fields, fork)
	s.appendEvent(inst.ID, EventReceived, map[string]any{
		"id": d.ID, "from": linkData(d.From), "fields": fields, "final": d.Final,
	})
	return "", s.checkSink()
}

func (s *Session) applyReceived(inst *Instance, d Delivery, fields map[string]any, fork *Fork) {
	s.received[d.ID] = true
	if inst.unconsumed == nil {
		inst.unconsumed = map[string]bool{}
	}
	for name := range fields {
		inst.unconsumed[name] = true
	}
	if fork != nil && d.Final {
		fork.Status = ForkCompleted
	}
	inst.exchange = append(inst.exchange, ExchangeItem{Fields: fields, Final: d.Final})
}

// awaitHeld reports whether an await gate still waits: some awaited field
// holds no unconsumed delivery.
func (inst *Instance) awaitHeld(step *Step) bool {
	for _, f := range step.Await {
		if !inst.unconsumed[f] {
			return true
		}
	}
	return false
}

// consume clears the unconsumed marks a passing await gate took.
func (inst *Instance) consume(fields []string) {
	for _, f := range fields {
		delete(inst.unconsumed, f)
	}
}

// roundCapReached reports whether a delegate arriving at an await gate has
// already made its declared number of interim deliveries: one more wait would
// open a round past the cap, so the delegate concludes instead.
func (s *Session) roundCapReached(inst *Instance, step *Step) bool {
	return s.Lineage != nil && len(step.Await) > 0 && inst.Spec.Rounds > 0 && inst.interim >= inst.Spec.Rounds
}

// deliver sends the step's delivered fields to the dispatching instance as
// the transition to `to` fires. It reports whether the transition may
// proceed: a refused delivery concludes the delegate instead, its work having
// nowhere to land.
func (s *Session) deliver(inst *Instance, step *Step, to string) (bool, error) {
	if to == EndAbandoned {
		return true, nil
	}
	if s.Lineage == nil {
		return false, fmt.Errorf("step %s delivers to the move that dispatched it, but %s is not a delegate session", step.ID, s.ID)
	}
	fields := make(map[string]any, len(step.Deliver))
	for _, f := range step.Deliver {
		v, ok := inst.Store.Get(f)
		if !ok {
			return false, fmt.Errorf("step %s delivers %q, which holds no value", step.ID, f)
		}
		fields[f] = v
	}
	d := Delivery{ID: s.deliveryID(inst), From: Link{Session: s.ID, Instance: inst.ID}, To: *s.Lineage, Fields: fields, Final: to == EndCompleted}
	refusal, err := s.send(d)
	if err != nil {
		return false, err
	}
	s.logDelivered(inst, d, refusal)
	if err := s.checkSink(); err != nil {
		return false, err
	}
	if refusal != "" {
		s.endInstance(inst, "undeliverable: "+refusal)
		return false, s.checkSink()
	}
	return true, nil
}

// sendFills delivers every send-mapped field the dispatching instance just
// wrote to the live forks awaiting it. A refusal ends that fork and stays as a
// named notice on the instance's serves.
func (s *Session) sendFills(inst *Instance, written []string) error {
	for _, fork := range inst.forks {
		if fork.Status != ForkLive || len(fork.Send) == 0 {
			continue
		}
		fields := map[string]any{}
		for _, childField := range slices.Sorted(maps.Keys(fork.Send)) {
			parentField := fork.Send[childField]
			if !slices.Contains(written, parentField) {
				continue
			}
			v, _ := inst.Store.Get(parentField)
			fields[childField] = v
		}
		if len(fields) == 0 {
			continue
		}
		d := Delivery{ID: s.deliveryID(inst), From: Link{Session: s.ID, Instance: inst.ID}, To: fork.Child, Fields: fields}
		refusal, err := s.send(d)
		if err != nil {
			return err
		}
		s.logDelivered(inst, d, refusal)
		if err := s.checkSink(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) send(d Delivery) (string, error) {
	if s.courier == nil {
		return "", fmt.Errorf("%s has no courier to reach %s", s.ID, d.To.Session)
	}
	refusal, err := s.courier.Deliver(d)
	if err != nil {
		return "", fmt.Errorf("delivering %s to %s: %w", d.ID, d.To.Session, err)
	}
	return refusal, nil
}

func (s *Session) deliveryID(inst *Instance) string {
	return fmt.Sprintf("%s/%s/%d", s.ID, inst.ID, inst.sent+1)
}

func (s *Session) logDelivered(inst *Instance, d Delivery, refusal string) {
	data := map[string]any{"id": d.ID, "to": linkData(d.To), "fields": d.Fields, "final": d.Final}
	if refusal != "" {
		data["refused"] = refusal
	}
	s.appendEvent(inst.ID, EventDelivered, data)
	s.applyDelivered(inst, d.To, d.Fields, d.Final, refusal)
}

func (s *Session) applyDelivered(inst *Instance, to Link, fields map[string]any, final bool, refusal string) {
	inst.sent++
	if refusal == "" && !final && s.Lineage != nil {
		inst.interim++
	}
	if refusal != "" {
		if fork := inst.fork(to); fork != nil && fork.Status == ForkLive {
			fork.Status, fork.Reason = ForkConcluded, refusal
		}
	}
	inst.exchange = append(inst.exchange, ExchangeItem{Sent: true, Fields: fields, Final: final, Refused: refusal})
}

// endInstance abandons a running instance from inside the engine, for a
// delegate whose exchange is over without a final delivery.
func (s *Session) endInstance(inst *Instance, reason string) {
	inst.Status = StatusAbandoned
	inst.Outcome = "abandoned"
	s.appendEvent(inst.ID, EventAbandoned, map[string]any{"reason": reason})
}

func (inst *Instance) fork(child Link) *Fork {
	for _, fork := range inst.forks {
		if fork.Child == child {
			return fork
		}
	}
	return nil
}

func (f *Fork) receiveField(childField string) (string, bool) {
	for parentField, child := range f.Receive {
		if child == childField {
			return parentField, true
		}
	}
	return "", false
}

// rejectDeliveryOnly refuses a report writing a field only a delivery fills.
func rejectDeliveryOnly(spec *Spec, fields map[string]any) error {
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		target := name
		if decl, ok := spec.State[name]; ok && decl.PatchOf != "" {
			target = decl.PatchOf
		}
		if spec.DeliveryOnly(target) {
			return fmt.Errorf("field %q is filled only by a delivery from the linked session — it cannot be reported", target)
		}
	}
	return nil
}

// awaitDiagnostic names what an await gate waits for, worded for whichever
// side of the exchange the session is.
func (s *Session) awaitDiagnostic(inst *Instance, step *Step) string {
	var waiting []string
	for _, f := range step.Await {
		if !inst.unconsumed[f] {
			waiting = append(waiting, f)
		}
	}
	if s.Lineage != nil {
		return "waiting for the dispatching move to send " + strings.Join(waiting, ", ") + " — end your turn; you are continued or relaunched with these same handles once it has, and next with this instance and no report then picks it up"
	}
	return "waiting for the delegate to deliver " + strings.Join(waiting, ", ") + " — next with this instance and no report picks the delivery up once it has landed"
}

// exchangeLane renders the full exchange a delegate instance took part in, so
// a delegate relaunched with fresh context loses nothing
// (20261109-004924-d-cpt-cxw). A dispatching instance's serves carry only its
// refused deliveries, as named notices.
func (s *Session) exchangeLane(inst *Instance) (*ServeLane, []truncate.Cut) {
	if len(inst.exchange) == 0 {
		return nil, nil
	}
	var b strings.Builder
	if s.Lineage == nil {
		for _, item := range inst.exchange {
			if item.Refused != "" {
				fmt.Fprintf(&b, "A delivery of %s to the delegate was not delivered: %s.\n", strings.Join(slices.Sorted(maps.Keys(item.Fields)), ", "), item.Refused)
			}
		}
		if b.Len() == 0 {
			return nil, nil
		}
		return &ServeLane{Name: "delivery", Text: strings.TrimSpace(b.String())}, nil
	}
	storeCap := serveview.Default().Cap(serveview.PartStoreValue)
	var cuts []truncate.Cut
	b.WriteString("The exchange with the dispatching move so far, oldest first:\n")
	for i, item := range inst.exchange {
		switch {
		case item.Sent && item.Final:
			fmt.Fprintf(&b, "\n%d. You delivered your final result:\n", i+1)
		case item.Sent:
			fmt.Fprintf(&b, "\n%d. You delivered:\n", i+1)
		default:
			fmt.Fprintf(&b, "\n%d. The dispatching move sent:\n", i+1)
		}
		for _, name := range slices.Sorted(maps.Keys(item.Fields)) {
			bounded, cut := serveview.BoundValue(item.Fields[name], storeCap)
			if cut != nil {
				cut.Part = fmt.Sprintf("exchange:%d:%s", i+1, name)
				cuts = append(cuts, *cut)
			}
			fmt.Fprintf(&b, "- %s: %s\n", name, exchangeValue(bounded))
		}
		if item.Refused != "" {
			fmt.Fprintf(&b, "  (not delivered: %s)\n", item.Refused)
		}
	}
	return &ServeLane{Name: "exchange", Text: strings.TrimSpace(b.String())}, cuts
}

func exchangeValue(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func linkData(l Link) map[string]any {
	return map[string]any{"session": l.Session, "instance": l.Instance}
}

func linkFrom(v any) Link {
	m, _ := v.(map[string]any)
	session, _ := m["session"].(string)
	instance, _ := m["instance"].(string)
	return Link{Session: session, Instance: instance}
}

func stringMapData(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func stringMapFrom(v any) map[string]string {
	m, _ := v.(map[string]any)
	out := make(map[string]string, len(m))
	for k, raw := range m {
		if s, ok := raw.(string); ok {
			out[k] = s
		}
	}
	return out
}

// applyDeliveryEvent folds the delivery events on replay.
func (s *Session) applyDeliveryEvent(ev Event) error {
	inst, err := s.replayInstance(ev)
	if err != nil {
		return err
	}
	switch ev.Event {
	case EventForked:
		procedure, _ := ev.Data["procedure"].(string)
		inst.forks = append(inst.forks, &Fork{
			Child: linkFrom(ev.Data["child"]), Procedure: procedure, Status: ForkLive,
			Receive: stringMapFrom(ev.Data["receive"]), Send: stringMapFrom(ev.Data["send"]),
		})
	case EventForkEnded:
		if fork := inst.fork(linkFrom(ev.Data["child"])); fork != nil {
			reason, _ := ev.Data["reason"].(string)
			fork.Status, fork.Reason = ForkConcluded, reason
		}
	case EventDelivered:
		fields, _ := ev.Data["fields"].(map[string]any)
		final, _ := ev.Data["final"].(bool)
		refused, _ := ev.Data["refused"].(string)
		s.applyDelivered(inst, linkFrom(ev.Data["to"]), fields, final, refused)
	case EventReceived:
		id, _ := ev.Data["id"].(string)
		fields, _ := ev.Data["fields"].(map[string]any)
		final, _ := ev.Data["final"].(bool)
		kept := declaredState(inst.Spec, fields)
		if _, err := inst.Store.WriteState(kept); err != nil {
			return err
		}
		from := linkFrom(ev.Data["from"])
		var fork *Fork
		if s.Lineage == nil || *s.Lineage != from {
			fork = inst.fork(from)
		}
		s.applyReceived(inst, Delivery{ID: id, From: from, Final: final}, kept, fork)
	}
	return nil
}

// startDelegateCheck refuses a start the delegate role does not allow: a
// delegate session runs exactly its one task, and a task that delivers runs
// only in a delegate session.
func (s *Session) startDelegateCheck(spec *Spec) error {
	if s.Lineage != nil {
		if len(s.instances) > 0 {
			return fmt.Errorf("delegate session %s runs only the task it was forked for", s.ID)
		}
		if spec.Class != model.ProcedureClassTask {
			return fmt.Errorf("delegate session %s runs a task, and %s is a %s", s.ID, spec.Canonical, spec.Class)
		}
		return nil
	}
	if spec.Delegates() {
		return fmt.Errorf("procedure %s delivers to the move that dispatched it, so it runs only in a delegate session forked on dispatch", spec.Canonical)
	}
	return nil
}
